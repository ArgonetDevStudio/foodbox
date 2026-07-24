#!/usr/bin/env bash
set -Eeuo pipefail

IFS=$'\n\t'
umask 077

command_name=${1:-}
job_id=${2:-}
foodbox_root=${FOODBOX_ROOT:-"$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"}
jobs_dir="$foodbox_root/.deploy-state/jobs"

if [[ ! $foodbox_root =~ ^/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+$ ]] || \
  [[ $(realpath -m -- "$foodbox_root") != "$foodbox_root" ]]; then
  echo "FOODBOX_ROOT must be a canonical absolute path with at least two components." >&2
  exit 2
fi

if [[ ! $job_id =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]; then
  echo "The deployment job ID is invalid." >&2
  exit 2
fi

job_dir="$jobs_dir/$job_id"
status_file="$job_dir/status"
pid_file="$job_dir/pid"
log_file="$job_dir/output.log"
signature_file="$job_dir/request.sha256"
launch_marker="$job_dir/launching"

write_status() {
  local value=$1
  local temporary
  temporary=$(mktemp "$job_dir/status.XXXXXX")
  printf '%s\n' "$value" >"$temporary"
  mv -f "$temporary" "$status_file"
}

hash_request() {
  printf '%s\0' "$@" | sha256sum | awk '{print $1}'
}

process_is_our_job() {
  local process_id=$1
  [[ $process_id =~ ^[0-9]+$ ]] || return 1
  kill -0 "$process_id" 2>/dev/null || return 1
  [[ -r /proc/$process_id/cmdline ]] || return 1
  tr '\0' '\n' <"/proc/$process_id/cmdline" | grep -Fxq "$job_id"
}

launch_job() {
  local job_pid
  local temporary_pid
  exec 8>"$job_dir/launch.lock"
  flock -w 15 8
  if [[ -f $status_file ]]; then
    echo "Deployment job $job_id has already completed."
    return 0
  fi
  if [[ -f $pid_file ]]; then
    job_pid=$(<"$pid_file")
    if process_is_our_job "$job_pid"; then
      echo "Deployment job $job_id is already running."
      return 0
    fi
    echo "Deployment job $job_id stopped without recording a result." >&2
    write_status 21
    return 0
  fi

  if [[ -f $launch_marker ]]; then
    echo "Deployment job $job_id has an indeterminate launch state." >&2
    write_status 21
    return 0
  fi

  printf 'launching\n' >"$launch_marker"
  nohup env FOODBOX_ROOT="$foodbox_root" \
    bash "$job_dir/job.sh" run "$job_id" \
    </dev/null >>"$log_file" 2>&1 8>&- &
  job_pid=$!
  temporary_pid=$(mktemp "$job_dir/pid.XXXXXX")
  printf '%s\n' "$job_pid" >"$temporary_pid"
  mv -f "$temporary_pid" "$pid_file"
  echo "Started deployment job $job_id with PID $job_pid."
}

case "$command_name" in
  start)
    operation=${3:-}
    shift 3
    mkdir -p "$jobs_dir"

    temporary_dir=$(mktemp -d "$jobs_dir/.job.XXXXXX")
    install -m 700 "$0" "$temporary_dir/job.sh"
    mkdir "$temporary_dir/bundle"

    case "$operation" in
      deploy)
        image_ref=${1:-}
        public_url=${2:-}
        staged_dir=${3:-}
        if [[ ! $image_ref =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]] ||
          [[ ! $public_url =~ ^https://[A-Za-z0-9.-]+/?$ ]] ||
          [[ $staged_dir != "$foodbox_root"/.incoming/* ]]; then
          rm -rf "$temporary_dir"
          echo "The deployment job arguments are invalid." >&2
          exit 2
        fi
        for relative_path in \
          docker-compose.yml deploy/Caddyfile scripts/deploy.sh scripts/rollback.sh scripts/job.sh \
          scripts/db_snapshot.py scripts/db_restore.py scripts/db_validate.py; do
          if [[ ! -f $staged_dir/$relative_path ]]; then
            rm -rf "$temporary_dir"
            echo "The staged deployment bundle is incomplete." >&2
            exit 2
          fi
        done
        mkdir -p "$temporary_dir/bundle/deploy" "$temporary_dir/bundle/scripts"
        install -m 600 "$staged_dir/docker-compose.yml" "$temporary_dir/bundle/docker-compose.yml"
        install -m 600 "$staged_dir/deploy/Caddyfile" "$temporary_dir/bundle/deploy/Caddyfile"
        install -m 700 "$staged_dir/scripts/deploy.sh" "$temporary_dir/bundle/scripts/deploy.sh"
        install -m 700 "$staged_dir/scripts/rollback.sh" "$temporary_dir/bundle/scripts/rollback.sh"
        install -m 700 "$staged_dir/scripts/job.sh" "$temporary_dir/bundle/scripts/job.sh"
        install -m 700 "$staged_dir/scripts/db_snapshot.py" "$temporary_dir/bundle/scripts/db_snapshot.py"
        install -m 700 "$staged_dir/scripts/db_restore.py" "$temporary_dir/bundle/scripts/db_restore.py"
        install -m 700 "$staged_dir/scripts/db_validate.py" "$temporary_dir/bundle/scripts/db_validate.py"
        bundle_digest=$(sha256sum "$temporary_dir/bundle/docker-compose.yml" \
          "$temporary_dir/bundle/deploy/Caddyfile" "$temporary_dir/bundle/scripts/"*.sh \
          "$temporary_dir/bundle/scripts/"*.py | awk '{print $1}')
        request_signature=$(hash_request "$operation" "$image_ref" "$public_url" "$bundle_digest")
        printf '%s\n' "$image_ref" >"$temporary_dir/image-ref"
        printf '%s\n' "$public_url" >"$temporary_dir/public-url"
        ;;
      rollback)
        public_url=${1:-}
        runner=${2:-"$foodbox_root/scripts/rollback.sh"}
        if [[ ! $public_url =~ ^https://[A-Za-z0-9.-]+/?$ ]] || [[ ! -f $runner ]]; then
          rm -rf "$temporary_dir"
          echo "The rollback job arguments are invalid." >&2
          exit 2
        fi
        install -m 700 "$runner" "$temporary_dir/bundle/rollback.sh"
        if [[ ! -f $foodbox_root/scripts/db_snapshot.py || ! -f $foodbox_root/scripts/db_restore.py || \
          ! -f $foodbox_root/scripts/db_validate.py ]]; then
          rm -rf "$temporary_dir"
          echo "The database snapshot helper is missing." >&2
          exit 2
        fi
        install -m 700 "$foodbox_root/scripts/db_snapshot.py" "$temporary_dir/bundle/db_snapshot.py"
        install -m 700 "$foodbox_root/scripts/db_restore.py" "$temporary_dir/bundle/db_restore.py"
        install -m 700 "$foodbox_root/scripts/db_validate.py" "$temporary_dir/bundle/db_validate.py"
        runner_digest=$(sha256sum "$temporary_dir/bundle/rollback.sh" | awk '{print $1}')
        snapshot_digest=$(sha256sum "$temporary_dir/bundle/db_snapshot.py" | awk '{print $1}')
        restore_digest=$(sha256sum "$temporary_dir/bundle/db_restore.py" | awk '{print $1}')
        validate_digest=$(sha256sum "$temporary_dir/bundle/db_validate.py" | awk '{print $1}')
        request_signature=$(hash_request "$operation" "$public_url" "$runner_digest" \
          "$snapshot_digest" "$restore_digest" "$validate_digest")
        printf '%s\n' "$public_url" >"$temporary_dir/public-url"
        ;;
      *)
        rm -rf "$temporary_dir"
        echo "The deployment job operation is invalid." >&2
        exit 2
        ;;
    esac
    printf '%s\n' "$operation" >"$temporary_dir/operation"
    printf '%s\n' "$request_signature" >"$temporary_dir/request.sha256"
    : >"$temporary_dir/output.log"

    if ! mv -T "$temporary_dir" "$job_dir" 2>/dev/null; then
      rm -rf "$temporary_dir"
      if [[ ! -f $signature_file ]] || [[ $(<"$signature_file") != "$request_signature" ]]; then
        echo "Job ID $job_id already belongs to a different request." >&2
        exit 2
      fi
      echo "Deployment job $job_id already exists; reattaching."
      launch_job
      exit 0
    fi

    launch_job
    ;;

  run)
    operation=$(<"$job_dir/operation")
    result=2
    # Invoked by the EXIT trap below.
    # shellcheck disable=SC2329
    finish_job() {
      local shell_status=$?
      if [[ ! -f $status_file ]]; then
        if [[ $result == 2 && $shell_status != 0 ]]; then
          result=$shell_status
        fi
        write_status "$result"
      fi
    }
    trap finish_job EXIT
    set +e
    if [[ $operation == deploy ]]; then
      image_ref=$(<"$job_dir/image-ref")
      public_url=$(<"$job_dir/public-url")
      bash "$job_dir/bundle/scripts/deploy.sh" "$image_ref" "$public_url" "$job_dir/bundle"
      result=$?
    elif [[ $operation == rollback ]]; then
      public_url=$(<"$job_dir/public-url")
      bash "$job_dir/bundle/rollback.sh" "$public_url"
      result=$?
    fi
    set -e
    exit "$result"
    ;;

  status)
    if [[ -f $status_file ]]; then
      result=$(<"$status_file")
      if [[ ! $result =~ ^[0-9]+$ ]]; then
        echo "INVALID"
        exit 2
      fi
      echo "EXIT:$result"
      exit 0
    fi
    if [[ -f $pid_file ]]; then
      running_pid=$(<"$pid_file")
      if process_is_our_job "$running_pid"; then
        echo "RUNNING"
        exit 0
      fi
      write_status 21
      echo "EXIT:21"
      exit 0
    fi
    if [[ -f $launch_marker ]]; then
      write_status 21
      echo "EXIT:21"
      exit 0
    fi
    echo "UNKNOWN"
    exit 2
    ;;

  log)
    if [[ -f $log_file ]]; then
      tail -n 100 "$log_file"
    else
      echo "No log exists for deployment job $job_id." >&2
      exit 2
    fi
    ;;

  *)
    echo "Usage: job.sh {start|status|log} JOB_ID ..." >&2
    exit 2
    ;;
esac
