#!/usr/bin/env bash
set -Eeuo pipefail

IFS=$'\n\t'
umask 077

image_ref=${1:-}
public_url=${2:-}
foodbox_root=${FOODBOX_ROOT:-"$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"}
incoming_dir="$foodbox_root/.incoming"
state_dir="$foodbox_root/.deploy-state"
backup_dir="$foodbox_root/backups"
compose_file="$foodbox_root/docker-compose.yml"
caddy_file="$foodbox_root/deploy/Caddyfile"
deploy_env="$foodbox_root/.deploy.env"

if [[ ! $image_ref =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]; then
  echo "The image must be an immutable GHCR sha256 digest." >&2
  exit 2
fi

if [[ ! $public_url =~ ^https://([A-Za-z0-9.-]+)(:[0-9]+)?/?$ ]]; then
  echo "The public URL must be an HTTPS origin without a path." >&2
  exit 2
fi
domain=${BASH_REMATCH[1]}

for required_file in \
  "$incoming_dir/docker-compose.yml" \
  "$incoming_dir/deploy/Caddyfile" \
  "$incoming_dir/scripts/deploy.sh" \
  "$incoming_dir/scripts/rollback.sh" \
  "$foodbox_root/.env"; do
  if [[ ! -f $required_file ]]; then
    echo "Required deployment file is missing: $required_file" >&2
    exit 2
  fi
done

mkdir -p "$foodbox_root/db" "$foodbox_root/deploy" "$foodbox_root/scripts" "$state_dir" "$backup_dir"
chmod 600 "$foodbox_root/.env"

exec 9>"$state_dir/deploy.lock"
if ! flock -w 300 9; then
  echo "Another deployment still holds the deployment lock." >&2
  exit 1
fi

transaction_dir=$(mktemp -d "$state_dir/transaction.XXXXXX")
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

restore_file() {
  local existed=$1
  local saved=$2
  local destination=$3

  if [[ $existed == true ]]; then
    install -m 600 "$saved" "$destination"
  else
    rm -f "$destination"
  fi
}

restore_previous_release() {
  echo "Restoring the release that was active before this deployment." >&2
  restore_file "$before_compose" "$transaction_dir/docker-compose.yml" "$compose_file"
  restore_file "$before_caddy" "$transaction_dir/Caddyfile" "$caddy_file"
  restore_file "$before_env" "$transaction_dir/deploy.env" "$deploy_env"

  if [[ $before_compose == true ]]; then
    if [[ $before_env == true ]]; then
      docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file" \
        up -d --remove-orphans --force-recreate --wait --wait-timeout 90 || true
    else
      docker compose --project-directory "$foodbox_root" -f "$compose_file" \
        up -d --remove-orphans --force-recreate || true
    fi
  fi
}

if [[ -f $foodbox_root/db/db.json ]]; then
  timestamp=$(date -u +%Y%m%dT%H%M%SZ)
  cp -p "$foodbox_root/db/db.json" "$backup_dir/db-$timestamp.json"
fi

echo "Pulling the immutable application image before changing the running release."
docker pull "$image_ref"

if ! docker run --rm --network none --entrypoint /bin/sh \
  -v "$foodbox_root/db:/data" "$image_ref" -c 'test -w /data'; then
  echo "Migrating the database bind mount to the non-root app UID/GID 10001."
  docker run --rm --network none --user 0 --entrypoint /bin/sh \
    -v "$foodbox_root/db:/data" "$image_ref" -c 'chown -R 10001:10001 /data'
fi

if ! docker run --rm --network none --entrypoint /bin/sh \
  -v "$foodbox_root/db:/data" "$image_ref" -c 'test -w /data'; then
  echo "The app user still cannot write to $foodbox_root/db after ownership migration." >&2
  exit 1
fi

install -m 600 "$incoming_dir/docker-compose.yml" "$compose_file"
install -m 600 "$incoming_dir/deploy/Caddyfile" "$caddy_file"
install -m 700 "$incoming_dir/scripts/deploy.sh" "$foodbox_root/scripts/deploy.sh"
install -m 700 "$incoming_dir/scripts/rollback.sh" "$foodbox_root/scripts/rollback.sh"

next_env=$(mktemp "$foodbox_root/.deploy.env.XXXXXX")
printf 'FOODBOX_IMAGE=%s\nDOMAIN=%s\n' "$image_ref" "$domain" >"$next_env"
chmod 600 "$next_env"
mv -f "$next_env" "$deploy_env"

compose=(docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file")

if ! "${compose[@]}" config --quiet; then
  restore_previous_release
  exit 1
fi

if ! "${compose[@]}" pull; then
  restore_previous_release
  exit 1
fi

deployment_ok=true
if ! "${compose[@]}" up -d --remove-orphans --force-recreate --wait --wait-timeout 90; then
  deployment_ok=false
fi

if [[ $deployment_ok == true ]] && ! curl \
  --fail --silent --show-error \
  --retry 12 --retry-delay 5 --retry-all-errors \
  --connect-timeout 5 --max-time 10 \
  "${public_url%/}/healthz" >/dev/null; then
  deployment_ok=false
fi

if [[ $deployment_ok != true ]]; then
  "${compose[@]}" ps >&2 || true
  "${compose[@]}" logs --tail 100 >&2 || true
  restore_previous_release
  exit 1
fi

rm -f "$state_dir/previous-compose.yml" "$state_dir/previous-Caddyfile" "$state_dir/previous.env"
if [[ $before_compose == true ]]; then
  install -m 600 "$transaction_dir/docker-compose.yml" "$state_dir/previous-compose.yml"
fi
if [[ $before_caddy == true ]]; then
  install -m 600 "$transaction_dir/Caddyfile" "$state_dir/previous-Caddyfile"
fi
if [[ $before_env == true ]]; then
  install -m 600 "$transaction_dir/deploy.env" "$state_dir/previous.env"
fi

rm -f "$transaction_dir/docker-compose.yml" "$transaction_dir/Caddyfile" "$transaction_dir/deploy.env"
rmdir "$transaction_dir"
rm -f \
  "$incoming_dir/docker-compose.yml" \
  "$incoming_dir/deploy/Caddyfile" \
  "$incoming_dir/scripts/deploy.sh" \
  "$incoming_dir/scripts/rollback.sh"

echo "Deployment completed and passed the public HTTPS health check."
