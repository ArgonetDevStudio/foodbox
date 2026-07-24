#!/usr/bin/env bash
set -Eeuo pipefail

IFS=$'\n\t'
umask 077

public_url=${1:-}
foodbox_root=${FOODBOX_ROOT:-"$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"}
state_dir="$foodbox_root/.deploy-state"
backup_dir="$foodbox_root/backups"
compose_file="$foodbox_root/docker-compose.yml"
caddy_file="$foodbox_root/deploy/Caddyfile"
deploy_env="$foodbox_root/.deploy.env"
previous_pointer="$state_dir/previous-release"
snapshot_helper="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/db_snapshot.py"
restore_helper="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/db_restore.py"
validate_helper="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/db_validate.py"
target_started=false
verified_db_backup=
transaction_dir=

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

for required_file in \
  "$compose_file" \
  "$caddy_file" \
  "$deploy_env" \
  "$previous_pointer" \
  "$foodbox_root/.env" \
  "$snapshot_helper" \
  "$restore_helper" \
  "$validate_helper"; do
  if [[ ! -f $required_file ]]; then
    echo "Rollback state is incomplete: $required_file" >&2
    exit 20
  fi
done

if [[ ! -d $foodbox_root/db || ! -f $foodbox_root/db/db.json ]]; then
  echo "The production database directory or db.json is missing." >&2
  exit 20
fi
if ! command -v python3 >/dev/null || ! command -v docker >/dev/null || ! \
  command -v flock >/dev/null || ! command -v curl >/dev/null || ! sudo -n true; then
  echo "Docker, curl, flock, python3, and passwordless sudo are required." >&2
  exit 20
fi

mkdir -p "$backup_dir"
canonical_backup_dir=$(realpath "$backup_dir")
exec 9>"$state_dir/deploy.lock"
if ! flock -w 300 9; then
  echo "Another deployment still holds the deployment lock." >&2
  exit 20
fi

previous_generation=$(<"$previous_pointer")
if [[ ! $previous_generation =~ ^release-[A-Za-z0-9]+$ ]]; then
  echo "Rollback state points to an invalid release generation." >&2
  exit 20
fi
previous_dir="$state_dir/releases/$previous_generation"
for required_file in \
  "$previous_dir/docker-compose.yml" \
  "$previous_dir/Caddyfile" \
  "$previous_dir/deploy.env"; do
  if [[ ! -f $required_file ]]; then
    echo "The previous Go release is incomplete: $required_file" >&2
    exit 20
  fi
done

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

ensure_compose_images() {
  local -a compose=("$@")
  local images
  local image
  images=$("${compose[@]}" config --images) || return 1
  [[ -n $images ]] || return 1
  while IFS= read -r image; do
    [[ $image =~ ^[A-Za-z0-9][A-Za-z0-9._/:@-]*@sha256:[a-f0-9]{64}$ ]] || return 1
    if ! docker image inspect "$image" >/dev/null 2>&1 && ! docker pull "$image" >/dev/null; then
      return 1
    fi
  done <<<"$images"
}

compose_is_stopped() {
  local -a compose=("$@")
  local containers
  local container
  local running
  containers=$("${compose[@]}" ps --all --quiet) || return 1
  while IFS= read -r container; do
    [[ -n $container ]] || continue
    running=$(docker inspect --format '{{.State.Running}}' "$container") || return 1
    [[ $running == false ]] || return 1
  done <<<"$containers"
}

exactly_one_writer_is_running() {
  local -a compose=("$@")
  local containers
  local container
  local running
  containers=$("${compose[@]}" ps --all --quiet app) || return 1
  [[ $(grep -cve '^$' <<<"$containers") == 1 ]] || return 1
  container=$(grep -ve '^$' <<<"$containers")
  running=$(docker inspect --format '{{.State.Running}}' "$container") || return 1
  [[ $running == true ]]
}

stop_compose_and_verify() {
  local -a compose=("$@")
  "${compose[@]}" stop --timeout 30 && compose_is_stopped "${compose[@]}"
}

probe_url() {
  local url=$1
  local expected=$2
  shift 2
  local body
  local status
  body=$(mktemp "$state_dir/probe.XXXXXX")
  status=$(curl --silent --show-error --output "$body" --write-out '%{http_code}' \
    --retry 12 --retry-delay 5 --retry-all-errors \
    --connect-timeout 5 --max-time 10 "$@" "$url") || {
    rm -f "$body"
    return 1
  }
  if [[ $status != 200 ]] || ! grep -Fq "$expected" "$body"; then
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
  local status
  body=$(mktemp "$state_dir/probe.XXXXXX")
  status=$(curl --silent --show-error --output "$body" --write-out '%{http_code}' \
    --retry 12 --retry-delay 5 --retry-all-errors \
    --connect-timeout 5 --max-time 10 "$@" "$url") || {
    rm -f "$body"
    return 1
  }
  if [[ $status != 200 ]] || ! python3 - "$contract" "$body" <<'PY'
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

release_is_healthy() {
  probe_json "https://${domain}/healthz" health --resolve "${domain}:443:127.0.0.1" &&
    probe_json "https://${domain}/api/menu" menu --resolve "${domain}:443:127.0.0.1" &&
    probe_url "https://${domain}/" '<div id="app">' --resolve "${domain}:443:127.0.0.1" &&
    probe_json "${public_url%/}/healthz" health &&
    probe_json "${public_url%/}/api/menu" menu &&
    probe_url "${public_url%/}/" '<div id="app">'
}

database_is_preserved() {
  sudo -n python3 "$validate_helper" "$verified_db_backup" "$foodbox_root/db"
}

database_matches_snapshot() {
  sudo -n python3 "$validate_helper" --exact "$verified_db_backup" "$foodbox_root/db"
}

snapshot_database() {
  sudo -n python3 "$snapshot_helper" "$foodbox_root/db" "$backup_dir" \
    "$(id -u)" "$(id -g)"
}

snapshot_is_valid() {
  local snapshot=$1
  local canonical_snapshot
  canonical_snapshot=$(realpath -m "$snapshot") || return 1
  [[ $snapshot == "$canonical_snapshot" ]] &&
    [[ $(dirname "$canonical_snapshot") == "$canonical_backup_dir" ]] &&
    [[ $(basename "$canonical_snapshot") =~ ^db-[0-9]{8}T[0-9]{6}Z-[0-9]+$ ]] &&
    [[ -f $canonical_snapshot/manifest.json ]] &&
    [[ -f $canonical_snapshot/data/db.json ]]
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
  if [[ $status != 200 ]] || ! sudo -n python3 - \
    "$verified_db_backup/data/db.json" "$foodbox_root/db/db.json" "$api_body" <<'PY'
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
        if not isinstance(date, list) or len(date) != 3 or any(type(item) is not int for item in date):
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

_, snapshot = load_database(snapshot_path)
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

install_release() {
  local release_dir=$1
  atomic_install "$release_dir/docker-compose.yml" "$compose_file" 600 &&
    atomic_install "$release_dir/Caddyfile" "$caddy_file" 600 &&
    atomic_install "$release_dir/deploy.env" "$deploy_env" 600
}

starting_compose=(
  docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
)
target_compose=(
  docker compose --project-directory "$foodbox_root" --env-file "$previous_dir/deploy.env" \
    -f "$previous_dir/docker-compose.yml"
)

if compgen -G "$state_dir/transaction.*" >/dev/null || \
  compgen -G "$state_dir/rollback.*" >/dev/null; then
  echo "An unresolved deployment or rollback transaction exists; inspect production before manual rollback." >&2
  exit 20
fi

if ! "${starting_compose[@]}" config --quiet || ! "${target_compose[@]}" config --quiet; then
  echo "The starting or target Go Compose configuration is invalid." >&2
  exit 20
fi

echo "Ensuring every immutable image for the target and recovery releases is available."
if ! ensure_compose_images "${target_compose[@]}" || ! ensure_compose_images "${starting_compose[@]}"; then
  echo "A rollback or recovery image is mutable, unavailable, or could not be pulled." >&2
  exit 20
fi

if ! verified_db_backup=$(snapshot_database); then
  echo "Could not create and verify a full database snapshot; rollback was not started." >&2
  exit 20
fi
if ! snapshot_is_valid "$verified_db_backup"; then
  echo "The database snapshot helper returned an invalid generation path." >&2
  exit 20
fi
echo "Verified database snapshot: $verified_db_backup"

transaction_dir=$(mktemp -d "$state_dir/rollback.XXXXXX")
preserve_transaction=false
cleanup_transaction() {
  local failed=false
  rm -f "$transaction_dir/docker-compose.yml" "$transaction_dir/Caddyfile" \
    "$transaction_dir/deploy.env" || failed=true
  rmdir "$transaction_dir" || failed=true
  [[ $failed == false ]]
}
finish_transaction_cleanup() {
  local outcome=$1
  if ! cleanup_transaction; then
    echo "The runtime outcome is known, but rollback transaction cleanup failed." >&2
    exit 11
  fi
  preserve_transaction=false
  exit "$outcome"
}
cleanup_safe_transaction_on_exit() {
  local status=$?
  trap - EXIT
  if [[ $preserve_transaction != true && -d $transaction_dir ]]; then
    if ! cleanup_transaction; then
      echo "Could not clear rollback transaction state." >&2
      exit 11
    fi
  fi
  exit "$status"
}
trap cleanup_safe_transaction_on_exit EXIT
install -m 600 "$compose_file" "$transaction_dir/docker-compose.yml"
install -m 600 "$caddy_file" "$transaction_dir/Caddyfile"
install -m 600 "$deploy_env" "$transaction_dir/deploy.env"

releases_dir="$state_dir/releases"
mkdir -p "$releases_dir"
next_state=$(mktemp -d "$releases_dir/.next.XXXXXX")
install -m 600 "$transaction_dir/docker-compose.yml" "$next_state/docker-compose.yml"
install -m 600 "$transaction_dir/Caddyfile" "$next_state/Caddyfile"
install -m 600 "$transaction_dir/deploy.env" "$next_state/deploy.env"
reciprocal_generation=$(basename "$next_state" | sed 's/^\.next\./release-/')
mv -T "$next_state" "$releases_dir/$reciprocal_generation"

restore_starting_release() {
  echo "The rollback failed; restoring the Go release that was active when it started." >&2

  if [[ $target_started == true ]]; then
    local -a failed_target=(
      docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
    )
    if ! stop_compose_and_verify "${failed_target[@]}"; then
      echo "The first target-stop attempt was not conclusive; retrying." >&2
    fi
    if ! compose_is_stopped "${failed_target[@]}" && \
      ! stop_compose_and_verify "${failed_target[@]}"; then
      echo "CRITICAL: the failed rollback target could not be stopped safely." >&2
      return 1
    fi
    target_started=false
  fi

  if ! database_is_preserved; then
    echo "Database integrity changed; restoring the complete verified snapshot." >&2
    if ! sudo -n python3 "$restore_helper" "$verified_db_backup" "$foodbox_root/db" "$backup_dir" || \
      ! database_matches_snapshot; then
      echo "CRITICAL: the verified database snapshot could not be restored safely." >&2
      return 1
    fi
  fi

  install_release "$transaction_dir" || return 1
  local -a recovery_compose=(
    docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
  )
  if ! "${recovery_compose[@]}" config --quiet || \
    ! "${recovery_compose[@]}" up \
      -d --remove-orphans --force-recreate --pull never --wait --wait-timeout 90; then
    echo "CRITICAL: the starting Go release did not restart." >&2
    "${recovery_compose[@]}" ps >&2 || true
    return 1
  fi
  if ! release_is_healthy || ! database_is_preserved || ! database_and_api_are_consistent || \
    ! exactly_one_writer_is_running "${recovery_compose[@]}"; then
    echo "CRITICAL: the restored Go release failed health, API, or database checks." >&2
    "${recovery_compose[@]}" ps >&2 || true
    return 1
  fi
  echo "The starting Go release was restored and verified." >&2
}

restart_starting_after_snapshot_failure() {
  echo "The final stopped-state snapshot failed; restarting the unchanged Go release." >&2
  if ! "${starting_compose[@]}" up \
    -d --remove-orphans --force-recreate --pull never --wait --wait-timeout 90; then
    echo "CRITICAL: the unchanged Go release did not restart." >&2
    "${starting_compose[@]}" ps >&2 || true
    return 1
  fi
  if ! database_is_preserved; then
    echo "CRITICAL: the restarted writer changed or lost data from the original verified snapshot." >&2
    if ! stop_compose_and_verify "${starting_compose[@]}"; then
      echo "CRITICAL: the restarted writer could not be stopped before database recovery." >&2
      return 1
    fi
    if ! sudo -n python3 "$restore_helper" "$verified_db_backup" "$foodbox_root/db" "$backup_dir" || \
      ! database_matches_snapshot; then
      echo "CRITICAL: the original verified snapshot could not be restored exactly." >&2
      return 1
    fi
    echo "The original database snapshot was restored exactly and the writer remains stopped." >&2
    return 1
  fi
  if ! release_is_healthy || ! database_is_preserved || ! database_and_api_are_consistent || \
    ! exactly_one_writer_is_running "${starting_compose[@]}"; then
    echo "CRITICAL: the restarted Go release failed health, API, database, or writer checks." >&2
    if ! stop_compose_and_verify "${starting_compose[@]}"; then
      echo "CRITICAL: the unverified writer could not be stopped safely." >&2
      "${starting_compose[@]}" ps >&2 || true
      return 1
    fi
    if ! database_is_preserved; then
      echo "Database integrity changed while the restarted release was being verified." >&2
      if ! sudo -n python3 "$restore_helper" "$verified_db_backup" "$foodbox_root/db" "$backup_dir" || \
        ! database_matches_snapshot; then
        echo "CRITICAL: the original verified snapshot could not be restored exactly." >&2
        return 1
      fi
      echo "The original database snapshot was restored exactly and the writer remains stopped." >&2
    fi
    "${starting_compose[@]}" ps >&2 || true
    return 1
  fi
  echo "The unchanged Go release was restarted and verified." >&2
}

rollback_ok=true
echo "Stopping the active Go stack before starting the previous writer."
preserve_transaction=true
if ! stop_compose_and_verify "${starting_compose[@]}"; then
  echo "The first stop attempt did not prove that every starting container stopped; retrying." >&2
  if ! stop_compose_and_verify "${starting_compose[@]}"; then
    echo "CRITICAL: the active writer could not be proven stopped; no target was started." >&2
    exit 11
  fi
fi

final_db_backup=
if ! final_db_backup=$(snapshot_database) || ! snapshot_is_valid "$final_db_backup"; then
  if restart_starting_after_snapshot_failure; then
    finish_transaction_cleanup 10
  fi
  echo "Manual intervention is required on the production server." >&2
  exit 11
fi
verified_db_backup=$final_db_backup
echo "Verified stopped-state database snapshot: $verified_db_backup"

if ! install_release "$previous_dir"; then
  rollback_ok=false
else
  active_target_compose=(
    docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file"
  )
  target_started=true
  if ! "${active_target_compose[@]}" config --quiet || \
    ! "${active_target_compose[@]}" up \
      -d --remove-orphans --force-recreate --pull never --wait --wait-timeout 90 || \
    ! release_is_healthy || ! database_is_preserved || ! database_and_api_are_consistent || \
    ! exactly_one_writer_is_running "${active_target_compose[@]}"; then
    rollback_ok=false
  fi
fi

if [[ $rollback_ok != true ]]; then
  if ! restore_starting_release; then
    echo "Manual intervention is required on the production server." >&2
    exit 11
  fi
  finish_transaction_cleanup 10
fi

next_pointer=$(mktemp "$state_dir/previous-release.XXXXXX")
printf '%s\n' "$reciprocal_generation" >"$next_pointer"
if ! mv -f "$next_pointer" "$previous_pointer"; then
  rm -f "$next_pointer"
  if ! restore_starting_release; then
    echo "Manual intervention is required on the production server." >&2
    exit 11
  fi
  finish_transaction_cleanup 10
fi

if ! cleanup_transaction; then
  echo "Rollback succeeded, but transaction cleanup failed." >&2
  exit 11
fi
preserve_transaction=false

echo "Go rollback completed and passed public /healthz, /api/menu, and / checks."
