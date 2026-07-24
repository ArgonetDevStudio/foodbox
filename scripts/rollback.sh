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

if [[ ! $public_url =~ ^https://([A-Za-z0-9.-]+)(:[0-9]+)?/?$ ]]; then
  echo "The public URL must be an HTTPS origin without a path." >&2
  exit 2
fi

mkdir -p "$backup_dir"

for required_file in \
  "$state_dir/previous-compose.yml" \
  "$state_dir/previous-Caddyfile" \
  "$state_dir/previous.env" \
  "$compose_file" \
  "$caddy_file" \
  "$deploy_env"; do
  if [[ ! -f $required_file ]]; then
    echo "Rollback state is incomplete: $required_file" >&2
    exit 2
  fi
done

previous_image=$(awk -F= '$1 == "FOODBOX_IMAGE" { print substr($0, index($0, "=") + 1) }' "$state_dir/previous.env")
if [[ ! $previous_image =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]; then
  echo "The stored rollback image is not an immutable GHCR digest." >&2
  exit 2
fi

exec 9>"$state_dir/deploy.lock"
if ! flock -w 300 9; then
  echo "Another deployment still holds the deployment lock." >&2
  exit 1
fi

transaction_dir=$(mktemp -d "$state_dir/rollback.XXXXXX")
cp -p "$compose_file" "$transaction_dir/docker-compose.yml"
cp -p "$caddy_file" "$transaction_dir/Caddyfile"
cp -p "$deploy_env" "$transaction_dir/deploy.env"

if [[ -f $foodbox_root/db/db.json ]]; then
  timestamp=$(date -u +%Y%m%dT%H%M%SZ)
  cp -p "$foodbox_root/db/db.json" "$backup_dir/db-rollback-$timestamp.json"
fi

restore_current_release() {
  echo "The rollback failed; restoring the release that was active when it started." >&2
  install -m 600 "$transaction_dir/docker-compose.yml" "$compose_file"
  install -m 600 "$transaction_dir/Caddyfile" "$caddy_file"
  install -m 600 "$transaction_dir/deploy.env" "$deploy_env"
  docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file" \
    up -d --remove-orphans --force-recreate --wait --wait-timeout 90 || true
}

install -m 600 "$state_dir/previous-compose.yml" "$compose_file"
install -m 600 "$state_dir/previous-Caddyfile" "$caddy_file"
install -m 600 "$state_dir/previous.env" "$deploy_env"

compose=(docker compose --project-directory "$foodbox_root" --env-file "$deploy_env" -f "$compose_file")

rollback_ok=true
if ! "${compose[@]}" config --quiet; then
  rollback_ok=false
elif ! "${compose[@]}" pull; then
  rollback_ok=false
elif ! "${compose[@]}" up -d --remove-orphans --force-recreate --wait --wait-timeout 90; then
  rollback_ok=false
elif ! curl \
  --fail --silent --show-error \
  --retry 12 --retry-delay 5 --retry-all-errors \
  --connect-timeout 5 --max-time 10 \
  "${public_url%/}/healthz" >/dev/null; then
  rollback_ok=false
fi

if [[ $rollback_ok != true ]]; then
  "${compose[@]}" ps >&2 || true
  "${compose[@]}" logs --tail 100 >&2 || true
  restore_current_release
  exit 1
fi

install -m 600 "$transaction_dir/docker-compose.yml" "$state_dir/previous-compose.yml"
install -m 600 "$transaction_dir/Caddyfile" "$state_dir/previous-Caddyfile"
install -m 600 "$transaction_dir/deploy.env" "$state_dir/previous.env"

rm -f "$transaction_dir/docker-compose.yml" "$transaction_dir/Caddyfile" "$transaction_dir/deploy.env"
rmdir "$transaction_dir"

echo "Rollback to $previous_image completed and passed the public HTTPS health check."
