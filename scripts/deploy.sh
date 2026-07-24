#!/usr/bin/env bash
set -Eeuo pipefail

IFS=$'\n\t'
umask 077

image_ref=${1:-}
public_url=${2:-}
foodbox_root=${FOODBOX_ROOT:-"$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"}
incoming_dir=${3:-"$foodbox_root/.incoming"}
state_dir="$foodbox_root/.deploy-state"
backup_dir="$foodbox_root/backups"
preflight_backup_dir="$state_dir/preflight-backups"
compose_file="$foodbox_root/docker-compose.yml"
caddy_file="$foodbox_root/deploy/Caddyfile"
deploy_env="$foodbox_root/.deploy.env"
release_changed=false
db_integrity_failed=false
target_started=false
starting_stack_stopped=false
previous_is_go=false
verified_db_backup=

if [[ ! $image_ref =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]; then
  echo "The image must be an immutable GHCR sha256 digest." >&2
  exit 2
fi

if [[ ! $public_url =~ ^https://([A-Za-z0-9.-]+)/?$ ]]; then
  echo "The public URL must use standard HTTPS port 443 and contain no path." >&2
  exit 2
fi
domain=${BASH_REMATCH[1]}

if [[ ! $foodbox_root =~ ^/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+$ ]] || \
  [[ $(realpath -m -- "$foodbox_root") != "$foodbox_root" ]]; then
  echo "FOODBOX_ROOT must be a canonical absolute path with at least two components." >&2
  exit 2
fi

canonical_incoming=$(realpath -m "$incoming_dir")
job_bundle=${canonical_incoming#"$state_dir/jobs/"}
if [[ $canonical_incoming != "$foodbox_root"/.incoming/* ]] && \
  [[ ! $job_bundle =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}/bundle$ ]]; then
  echo "The staging directory must be inside $foodbox_root/.incoming." >&2
  exit 2
fi
incoming_dir=$canonical_incoming
snapshot_helper="$incoming_dir/scripts/db_snapshot.py"
restore_helper="$incoming_dir/scripts/db_restore.py"
validate_helper="$incoming_dir/scripts/db_validate.py"

for required_file in \
  "$incoming_dir/docker-compose.yml" \
  "$incoming_dir/deploy/Caddyfile" \
  "$incoming_dir/scripts/deploy.sh" \
  "$incoming_dir/scripts/rollback.sh" \
  "$incoming_dir/scripts/job.sh" \
  "$incoming_dir/scripts/db_snapshot.py" \
  "$incoming_dir/scripts/db_restore.py" \
  "$incoming_dir/scripts/db_validate.py" \
  "$foodbox_root/.env"; do
  if [[ ! -f $required_file ]]; then
    echo "Required deployment file is missing: $required_file" >&2
    exit 2
  fi
done

if [[ ! -d $foodbox_root/db || ! -f $foodbox_root/db/db.json ]]; then
  echo "The production database directory or db.json is missing; refusing to create empty state." >&2
  exit 20
fi

mkdir -p "$foodbox_root/deploy" "$foodbox_root/scripts" "$state_dir" "$backup_dir" \
  "$preflight_backup_dir"

atomic_install() {
  local source=$1
  local destination=$2
  local mode=$3
  local temporary
  temporary=$(mktemp "$(dirname "$destination")/.$(basename "$destination").XXXXXX")
  if ! install -m "$mode" "$source" "$temporary"; then
    rm -f "$temporary"
    return 1
  fi
  mv -f "$temporary" "$destination"
}

snapshot_database() {
  local destination=${1:-$backup_dir}
  sudo -n python3 "$snapshot_helper" "$foodbox_root/db" "$destination" \
    "$(id -u)" "$(id -g)"
}

restore_database_after_integrity_failure() {
  sudo -n python3 "$restore_helper" "$verified_db_backup" "$foodbox_root/db" "$backup_dir"
}

database_matches_snapshot() {
  sudo -n python3 "$validate_helper" --exact "$verified_db_backup" "$foodbox_root/db"
}

compose_is_stopped() {
  local -a compose_command=("$@")
  local containers
  local container
  local running
  containers=$("${compose_command[@]}" ps --all --quiet) || return 1
  while IFS= read -r container; do
    [[ -n $container ]] || continue
    running=$(docker inspect --format '{{.State.Running}}' "$container") || return 1
    [[ $running == false ]] || return 1
  done <<<"$containers"
}

stop_compose_and_verify() {
  local -a compose_command=("$@")
  "${compose_command[@]}" stop --timeout 30 && compose_is_stopped "${compose_command[@]}"
}

exactly_one_writer_is_running() {
  local -a compose_command=("$@")
  local containers
  local container
  local running
  containers=$("${compose_command[@]}" ps --all --quiet app) || return 1
  [[ $(grep -cve '^$' <<<"$containers") == 1 ]] || return 1
  container=$(grep -ve '^$' <<<"$containers")
  running=$(docker inspect --format '{{.State.Running}}' "$container") || return 1
  [[ $running == true ]]
}

exec 9>"$state_dir/deploy.lock"
if ! flock -w 300 9; then
  echo "Another deployment still holds the deployment lock." >&2
  exit 20
fi

if compgen -G "$state_dir/transaction.*" >/dev/null || \
  compgen -G "$state_dir/rollback.*" >/dev/null; then
  echo "An unresolved deployment or rollback transaction exists; inspect production before deployment." >&2
  exit 20
fi

chmod 600 "$foodbox_root/.env"
transaction_dir=$(mktemp -d "$state_dir/transaction.XXXXXX")
preserve_transaction=false
cleanup_safe_transaction_on_exit() {
  local status=$?
  trap - EXIT
  if [[ $preserve_transaction != true && -d $transaction_dir ]]; then
    rm -f "$transaction_dir/docker-compose.yml" "$transaction_dir/Caddyfile" \
      "$transaction_dir/deploy.env"
    if ! rmdir "$transaction_dir"; then
      echo "Could not clear deployment transaction state." >&2
      exit 11
    fi
  fi
  exit "$status"
}
trap cleanup_safe_transaction_on_exit EXIT
before_compose=false
before_caddy=false
before_env=false

if [[ -f $compose_file ]]; then
  cp -p "$compose_file" "$transaction_dir/docker-compose.yml"
  before_compose=true
fi
if [[ -f $caddy_file ]]; then
  cp -p "$caddy_file" "$transaction_dir/Caddyfile"
  before_caddy=true
fi
if [[ -f $deploy_env ]]; then
  cp -p "$deploy_env" "$transaction_dir/deploy.env"
  before_env=true
fi

if [[ $before_compose != true ]]; then
  echo "The active production Compose file is missing; refusing a deployment without rollback state." >&2
  exit 20
fi
if [[ $before_env == true && $before_caddy != true ]]; then
  echo "The active Go release has no Caddy configuration; refusing to change it." >&2
  exit 20
fi
if [[ $before_env == true ]]; then
  previous_is_go=true
  current_compose=(
    docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
  )
else
  current_compose=(
    docker compose --project-directory "$foodbox_root" --env-file "$foodbox_root/.env" -f "$compose_file"
  )
fi
if ! "${current_compose[@]}" config --quiet; then
  echo "The active production Compose configuration is invalid; the release was not changed." >&2
  exit 20
fi

restore_file() {
  local existed=$1
  local saved=$2
  local destination=$3

  if [[ $existed == true ]]; then
    atomic_install "$saved" "$destination" 600
  else
    rm -f "$destination"
  fi
}

restore_previous_release() {
  if [[ $target_started == true && -f $deploy_env && -f $compose_file ]]; then
    local failed_compose=(
      docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
    )
    if ! stop_compose_and_verify "${failed_compose[@]}"; then
      echo "The first target-stop attempt was not conclusive; retrying." >&2
    fi
    if ! compose_is_stopped "${failed_compose[@]}" && \
      ! stop_compose_and_verify "${failed_compose[@]}"; then
      echo "The failed release could not be stopped safely." >&2
      return 1
    fi
  elif [[ $starting_stack_stopped != true ]]; then
    if ! stop_compose_and_verify "${current_compose[@]}"; then
      echo "The starting release could not be confirmed stopped; database recovery was not attempted." >&2
      return 1
    fi
  fi
  if [[ $previous_is_go != true ]]; then
    db_integrity_failed=true
  elif ! database_is_preserved; then
    db_integrity_failed=true
  fi
  if [[ $db_integrity_failed == true ]]; then
    if ! restore_database_after_integrity_failure || ! database_matches_snapshot; then
      echo "The verified database snapshot could not be restored and validated safely." >&2
      return 1
    fi
  fi

  if [[ $previous_is_go != true ]]; then
    restore_file "$before_compose" "$transaction_dir/docker-compose.yml" "$compose_file" || return 1
    restore_file "$before_caddy" "$transaction_dir/Caddyfile" "$caddy_file" || return 1
    restore_file "$before_env" "$transaction_dir/deploy.env" "$deploy_env" || return 1
    if ! compose_is_stopped "${current_compose[@]}" || ! database_matches_snapshot; then
      echo "The first cutover recovery could not prove an exact stopped state." >&2
      return 1
    fi
    echo "The first Go cutover failed. The failed target is stopped, retry state and the database are protected, and the retired runtime will not be restarted." >&2
    return 2
  fi

  echo "Restoring the exact Go release that was active before this deployment." >&2
  restore_file "$before_compose" "$transaction_dir/docker-compose.yml" "$compose_file" || return 1
  restore_file "$before_caddy" "$transaction_dir/Caddyfile" "$caddy_file" || return 1
  restore_file "$before_env" "$transaction_dir/deploy.env" "$deploy_env" || return 1

  local recovery_ok=true
  local recovery_compose=(
    docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
  )
  if ! "${recovery_compose[@]}" config --quiet; then
    recovery_ok=false
  elif ! ensure_compose_images_available "${recovery_compose[@]}"; then
    recovery_ok=false
  elif ! "${recovery_compose[@]}" up \
    -d --remove-orphans --force-recreate --pull never --wait --wait-timeout 90; then
    recovery_ok=false
  elif ! go_release_is_healthy; then
    recovery_ok=false
  elif ! database_and_api_are_consistent; then
    recovery_ok=false
  elif ! exactly_one_writer_is_running "${recovery_compose[@]}"; then
    recovery_ok=false
  fi

  if [[ $recovery_ok != true ]]; then
    echo "CRITICAL: deployment failed and the previous release did not recover successfully." >&2
    "${recovery_compose[@]}" ps >&2 || true
    return 1
  fi

  echo "The previous Go release is running and passed disk, API, health, and UI checks." >&2
}

cleanup_transaction() {
  rm -f "$transaction_dir/docker-compose.yml" "$transaction_dir/Caddyfile" \
    "$transaction_dir/deploy.env"
  rmdir "$transaction_dir"
}

probe_url() {
  local url=$1
  local expected=$2
  shift 2
  local body
  body=$(mktemp "$state_dir/probe.XXXXXX")
  if ! curl \
    --silent --show-error --output "$body" --write-out '%{http_code}' \
    --retry 12 --retry-delay 5 --retry-all-errors \
    --connect-timeout 5 --max-time 10 \
    "$@" "$url" | grep -qx '200'; then
    rm -f "$body"
    return 1
  fi
  if ! grep -Fq "$expected" "$body"; then
    rm -f "$body"
    return 1
  fi
  rm -f "$body"
}

probe_json() {
  local url=$1
  local contract=$2
  shift 2
  local body
  body=$(mktemp "$state_dir/probe.XXXXXX")
  if ! curl \
    --silent --show-error --output "$body" --write-out '%{http_code}' \
    --retry 12 --retry-delay 5 --retry-all-errors \
    --connect-timeout 5 --max-time 10 \
    "$@" "$url" | grep -qx '200'; then
    rm -f "$body"
    return 1
  fi
  if ! python3 - "$contract" "$body" <<'PY'
import json
import sys

contract, path = sys.argv[1:]
with open(path, encoding="utf-8") as response:
    value = json.load(response)
if not isinstance(value, dict) or set(value) != {"status", "error", "data"}:
    raise SystemExit(1)
if value["status"] != 200 or value["error"] is not None:
    raise SystemExit(1)
if contract == "health":
    if value["data"] != {"ready": True}:
        raise SystemExit(1)
elif contract == "menu":
    if not isinstance(value["data"], list):
        raise SystemExit(1)
else:
    raise SystemExit(1)
PY
  then
    rm -f "$body"
    return 1
  fi
  rm -f "$body"
}

go_release_is_healthy() {
  probe_json "https://${domain}/healthz" health --resolve "${domain}:443:127.0.0.1" &&
    probe_json "https://${domain}/api/menu" menu --resolve "${domain}:443:127.0.0.1" &&
    probe_url "https://${domain}/" '<div id="app">' --resolve "${domain}:443:127.0.0.1" &&
    probe_json "${public_url%/}/healthz" health &&
    probe_json "${public_url%/}/api/menu" menu &&
    probe_url "${public_url%/}/" '<div id="app">'
}

database_and_api_are_consistent() {
  local api_body
  local status
  api_body=$(mktemp "$state_dir/api.XXXXXX")
  status=$(curl --silent --show-error --output "$api_body" --write-out '%{http_code}' \
    --connect-timeout 5 --max-time 15 --resolve "${domain}:443:127.0.0.1" \
    "https://${domain}/api/menu") || {
    rm -f "$api_body"
    return 1
  }
  if [[ $status != 200 ]]; then
    rm -f "$api_body"
    return 1
  fi
  if ! sudo -n python3 - "$verified_db_backup/data/db.json" "$foodbox_root/db/db.json" "$api_body" <<'PY'
import datetime
import json
import sys

snapshot_path, current_path, api_path = sys.argv[1:]

def load_database(path):
    with open(path, encoding="utf-8") as database:
        rows = json.load(database)
    if not isinstance(rows, list):
        raise SystemExit(1)
    result = {}
    previous = None
    for row in rows:
        if not isinstance(row, dict) or set(row) != {"date", "menus", "valid"}:
            raise SystemExit(1)
        date = row["date"]
        if not isinstance(date, list) or len(date) != 3 or any(type(value) is not int for value in date):
            raise SystemExit(1)
        parsed = datetime.date(*date)
        if parsed in result or (previous is not None and parsed < previous):
            raise SystemExit(1)
        if not isinstance(row["menus"], list) or any(not isinstance(item, str) for item in row["menus"]):
            raise SystemExit(1)
        if type(row["valid"]) is not bool:
            raise SystemExit(1)
        result[parsed] = row
        previous = parsed
    return rows, result

snapshot_rows, snapshot = load_database(snapshot_path)
current_rows, current = load_database(current_path)
for date, row in snapshot.items():
    if current.get(date) != row:
        raise SystemExit(1)
with open(api_path, encoding="utf-8") as response:
    envelope = json.load(response)
if not isinstance(envelope, dict) or set(envelope) != {"status", "error", "data"}:
    raise SystemExit(1)
if envelope["status"] != 200 or envelope["error"] is not None or not isinstance(envelope["data"], list):
    raise SystemExit(1)
expected = [{
    "date": f'{row["date"][0]:04d}-{row["date"][1]:02d}-{row["date"][2]:02d}',
    "menus": row["menus"],
    "isValid": row["valid"],
} for row in reversed(current_rows)]
if envelope["data"] != expected:
    raise SystemExit(1)
PY
  then
    rm -f "$api_body"
    return 1
  fi
  rm -f "$api_body"
}

database_is_preserved() {
  sudo -n python3 "$validate_helper" "$verified_db_backup" "$foodbox_root/db"
}

ensure_compose_images_available() {
  local -a compose_command=("$@")
  local images_output
  local image
  if ! images_output=$("${compose_command[@]}" config --images) || [[ -z $images_output ]]; then
    return 1
  fi
  while IFS= read -r image; do
    [[ -n $image ]] || continue
    if [[ ! $image =~ ^[A-Za-z0-9._/-]+:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$ ]] && \
      [[ ! $image =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]; then
      echo "Recovery image is not pinned by digest: $image" >&2
      return 1
    fi
    if ! docker image inspect "$image" >/dev/null 2>&1 && ! docker pull "$image" >/dev/null; then
      return 1
    fi
  done <<<"$images_output"
}

recover_or_exit() {
  local recovery_status
  trap - ERR
  if restore_previous_release; then
    if ! cleanup_transaction; then
      echo "Recovered the runtime, but could not clear deployment transaction state." >&2
      exit 11
    fi
    echo "Deployment failed, but the previous release was verified healthy." >&2
    exit 10
  else
    recovery_status=$?
  fi
  if [[ $previous_is_go != true && $recovery_status == 2 ]]; then
    if ! cleanup_transaction; then
      echo "Recovered the stopped first-cutover state, but could not clear deployment transaction state." >&2
      exit 11
    fi
    echo "The first Go cutover is safely stopped. Correct the failure and retry deployment." >&2
    exit 12
  fi
  echo "Manual intervention is required on the production server." >&2
  exit 11
}

handle_unexpected_error() {
  local line_number=$1
  trap - ERR
  echo "Deployment command failed unexpectedly near line $line_number." >&2
  if [[ $release_changed == true ]]; then
    if restore_previous_release; then
      if ! cleanup_transaction; then
        echo "Recovered the runtime, but could not clear deployment transaction state." >&2
        exit 11
      fi
      echo "The previous release recovered after an unexpected deployment failure." >&2
      exit 10
    else
      local recovery_status=$?
    fi
    if [[ $previous_is_go != true && $recovery_status == 2 ]]; then
      if ! cleanup_transaction; then
        echo "Recovered the stopped first-cutover state, but could not clear deployment transaction state." >&2
        exit 11
      fi
      echo "The first Go cutover is safely stopped. Correct the failure and retry deployment." >&2
      exit 12
    fi
    echo "Manual intervention is required on the production server." >&2
    exit 11
  fi
  if [[ $starting_stack_stopped == true ]]; then
    echo "The starting stack was stopped before transaction state became recoverable; manual inspection is required." >&2
    exit 11
  fi
  exit 20
}

trap 'handle_unexpected_error "$LINENO"' ERR

if ! command -v python3 >/dev/null; then
  echo "python3 is required for production response validation." >&2
  exit 20
fi
if ! sudo -n true; then
  echo "Passwordless sudo is required for atomic database integrity recovery." >&2
  exit 20
fi

if [[ $previous_is_go == true ]]; then
  echo "Verifying that every immutable image for the active Go release is recoverable."
  if ! ensure_compose_images_available "${current_compose[@]}"; then
    echo "The active Go release cannot be reproduced from immutable images; the release was not changed." >&2
    exit 20
  fi
fi

echo "Ensuring immutable application and Caddy images are available before changing the release."
if ! docker image inspect "$image_ref" >/dev/null 2>&1 && ! docker pull "$image_ref"; then
  echo "The immutable application image is unavailable locally and could not be pulled." >&2
  exit 20
fi

caddy_image=$(awk '$1 == "image:" && $2 ~ /^caddy:[^@]+@sha256:[a-f0-9]+$/ { print $2; exit }' \
  "$incoming_dir/docker-compose.yml")
if [[ ! $caddy_image =~ ^caddy:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$ ]]; then
  echo "The staged Compose file has no immutable Caddy image." >&2
  exit 20
fi
if ! docker image inspect "$caddy_image" >/dev/null 2>&1 && ! docker pull "$caddy_image"; then
  echo "The immutable Caddy image is unavailable locally and could not be pulled." >&2
  exit 20
fi

if ! verified_db_backup=$(snapshot_database "$preflight_backup_dir"); then
  echo "Could not create and verify the pre-stop database snapshot; the active release was not changed." >&2
  exit 20
fi
if [[ $verified_db_backup != "$preflight_backup_dir"/db-* || \
  ! -f $verified_db_backup/manifest.json ]]; then
  echo "The database snapshot helper returned an invalid pre-stop generation path." >&2
  exit 20
fi
echo "Verified pre-stop database snapshot: $verified_db_backup"

rollback_generation=
if [[ $previous_is_go == true ]]; then
  releases_dir="$state_dir/releases"
  mkdir -p "$releases_dir"
  next_state=$(mktemp -d "$releases_dir/.next.XXXXXX")
  install -m 600 "$transaction_dir/docker-compose.yml" "$next_state/docker-compose.yml"
  install -m 600 "$transaction_dir/Caddyfile" "$next_state/Caddyfile"
  install -m 600 "$transaction_dir/deploy.env" "$next_state/deploy.env"
  printf 'go\n' >"$next_state/kind"
  chmod 600 "$next_state"/*
  rollback_generation=$(basename "$next_state" | sed 's/^\.next\./release-/')
  mv -T "$next_state" "$releases_dir/$rollback_generation"
fi

if [[ $before_compose == true ]]; then
  if [[ $before_env == true ]]; then
    starting_compose=(
      docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
    )
  else
    starting_compose=(
      docker compose --project-directory "$foodbox_root" --env-file "$foodbox_root/.env" -f "$compose_file"
    )
  fi
  echo "Stopping the current stack before changing database ownership or starting the new writer."
  preserve_transaction=true
  if ! stop_compose_and_verify "${starting_compose[@]}"; then
    echo "The first stop attempt did not prove that every starting container stopped; retrying." >&2
    if ! stop_compose_and_verify "${starting_compose[@]}"; then
      echo "The starting writer could not be confirmed stopped; manual intervention is required." >&2
      exit 11
    fi
  fi
  starting_stack_stopped=true

  final_db_backup=
  if ! final_db_backup=$(snapshot_database); then
    echo "The writer is stopped, but the final database snapshot failed; manual intervention is required." >&2
    exit 11
  fi
  if [[ $final_db_backup != "$backup_dir"/db-* || ! -f $final_db_backup/manifest.json ]]; then
    echo "The writer is stopped, but the snapshot helper returned an invalid final generation path." >&2
    exit 11
  fi
  verified_db_backup=$final_db_backup
  echo "Verified final stopped-writer database snapshot: $verified_db_backup"
fi
release_changed=true

if ! docker run --rm --network none --entrypoint /bin/sh \
  -v "$foodbox_root/db:/data" "$image_ref" -c 'test -w /data'; then
  echo "Migrating the database bind mount to the non-root app UID/GID 10001."
  docker run --rm --network none --user 0 --entrypoint /bin/sh \
    -v "$foodbox_root/db:/data" "$image_ref" -c 'chown -R 10001:10001 /data'
fi

if ! docker run --rm --network none --entrypoint /bin/sh \
  -v "$foodbox_root/db:/data" "$image_ref" -c 'test -w /data'; then
  echo "The app user still cannot write to $foodbox_root/db after ownership migration." >&2
  recover_or_exit
fi

atomic_install "$incoming_dir/docker-compose.yml" "$compose_file" 600
atomic_install "$incoming_dir/deploy/Caddyfile" "$caddy_file" 600
atomic_install "$incoming_dir/scripts/deploy.sh" "$foodbox_root/scripts/deploy.sh" 700
atomic_install "$incoming_dir/scripts/rollback.sh" "$foodbox_root/scripts/rollback.sh" 700
atomic_install "$incoming_dir/scripts/job.sh" "$foodbox_root/scripts/job.sh" 700
atomic_install "$incoming_dir/scripts/db_snapshot.py" "$foodbox_root/scripts/db_snapshot.py" 700
atomic_install "$incoming_dir/scripts/db_restore.py" "$foodbox_root/scripts/db_restore.py" 700
atomic_install "$incoming_dir/scripts/db_validate.py" "$foodbox_root/scripts/db_validate.py" 700

next_env=$(mktemp "$foodbox_root/.deploy.env.XXXXXX")
printf 'FOODBOX_IMAGE=%s\nDOMAIN=%s\n' "$image_ref" "$domain" >"$next_env"
chmod 600 "$next_env"
mv -f "$next_env" "$deploy_env"

compose=(docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file")

if ! "${compose[@]}" config --quiet; then
  recover_or_exit
fi

deployment_ok=true
target_started=true
if ! "${compose[@]}" up \
  -d --remove-orphans --force-recreate --pull never --wait --wait-timeout 90; then
  deployment_ok=false
fi

if ! database_is_preserved; then
  echo "The new release changed or invalidated preserved database records." >&2
  db_integrity_failed=true
  deployment_ok=false
fi

if [[ $deployment_ok == true ]] && ! go_release_is_healthy; then
  deployment_ok=false
fi

if [[ $deployment_ok == true ]] && ! database_and_api_are_consistent; then
  echo "Database preservation or API parity validation failed." >&2
  deployment_ok=false
fi

if [[ $deployment_ok == true ]] && ! exactly_one_writer_is_running "${compose[@]}"; then
  echo "The new release does not have exactly one running database writer." >&2
  deployment_ok=false
fi

if [[ $deployment_ok != true ]]; then
  "${compose[@]}" ps >&2 || true
  "${compose[@]}" logs --tail 100 >&2 || true
  recover_or_exit
fi

if [[ -n $rollback_generation ]]; then
  next_pointer=$(mktemp "$state_dir/previous-release.XXXXXX")
  printf '%s\n' "$rollback_generation" >"$next_pointer"
  mv -f "$next_pointer" "$state_dir/previous-release"
else
  rm -f "$state_dir/previous-release"
fi

release_changed=false
trap - ERR

cleanup_transaction
preserve_transaction=false
rm -f \
  "$incoming_dir/docker-compose.yml" \
  "$incoming_dir/deploy/Caddyfile" \
  "$incoming_dir/scripts/deploy.sh" \
  "$incoming_dir/scripts/rollback.sh" \
  "$incoming_dir/scripts/job.sh" \
  "$incoming_dir/scripts/db_snapshot.py" \
  "$incoming_dir/scripts/db_restore.py" \
  "$incoming_dir/scripts/db_validate.py"
rmdir "$incoming_dir/deploy" "$incoming_dir/scripts" "$incoming_dir" 2>/dev/null || true

echo "Deployment completed and passed public /healthz, /api/menu, and / checks."
