#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
compose_file="$repo_root/manifest/docker/docker-compose.yml"
migrations_dir="$repo_root/manifest/sql/migrations"
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/wisesentinel-migrations-XXXXXX")
lock_output=$(mktemp "${TMPDIR:-/tmp}/wisesentinel-migration-lock-XXXXXX")
holder_pid=''
synthetic_migration_name=''

cleanup() {
  if [[ -n "$holder_pid" ]] && kill -0 "$holder_pid" 2>/dev/null; then
    kill "$holder_pid" 2>/dev/null || true
  fi
  if [[ -n "$holder_pid" ]]; then
    wait "$holder_pid" 2>/dev/null || true
  fi
  if [[ -n "$synthetic_migration_name" ]]; then
    docker compose -f "$compose_file" exec -T mysql \
      mysql -uws -pws123 wisesentinel \
      -e "DELETE FROM ws_schema_migration WHERE migration_name = '$synthetic_migration_name'" >/dev/null 2>&1 || true
  fi
  rm -rf "$fixture_dir"
  rm -f "$lock_output"
}
trap cleanup EXIT

docker compose -f "$compose_file" run --rm migrate
docker compose -f "$compose_file" run --rm migrate

expected_count=$(find "$migrations_dir" -maxdepth 1 -type f -name '*.sql' | wc -l | tr -d ' ')
actual_count=$(docker compose -f "$compose_file" exec -T mysql \
  mysql -N -B -uws -pws123 wisesentinel \
  -e "SELECT COUNT(*) FROM ws_schema_migration")
if [[ "$actual_count" != "$expected_count" ]]; then
  echo "migration ledger count mismatch: expected=$expected_count actual=$actual_count" >&2
  exit 1
fi

synthetic_migration_name="zz_test_missing_migration_${RANDOM}_$$.sql"
docker compose -f "$compose_file" exec -T mysql \
  mysql -uws -pws123 wisesentinel \
  -e "INSERT INTO ws_schema_migration (migration_name, checksum) VALUES ('$synthetic_migration_name', REPEAT('0', 64))"
set +e
artifact_rejection=$(docker compose -f "$compose_file" run --rm migrate 2>&1)
artifact_rejection_status=$?
set -e
if [[ $artifact_rejection_status -eq 0 ]]; then
  echo 'expected deployment artifact missing recorded migration to be rejected' >&2
  exit 1
fi
if ! grep -Fq "migration history is not present in deployment artifact: $synthetic_migration_name" <<<"$artifact_rejection"; then
  echo 'missing migration history rejection lacked the expected diagnostic' >&2
  printf '%s\n' "$artifact_rejection" >&2
  exit 1
fi
docker compose -f "$compose_file" exec -T mysql \
  mysql -uws -pws123 wisesentinel \
  -e "DELETE FROM ws_schema_migration WHERE migration_name = '$synthetic_migration_name'"
synthetic_migration_name=''

docker compose -f "$compose_file" run --rm -e MIGRATION_LOCK_HOLD_SECONDS=3 migrate >"$lock_output" 2>&1 &
holder_pid=$!
for _ in $(seq 1 100); do
  if grep -Fq 'migration lock acquired' "$lock_output"; then
    break
  fi
  if ! kill -0 "$holder_pid" 2>/dev/null; then
    cat "$lock_output" >&2
    echo 'migration lock holder exited before the concurrency regression started' >&2
    exit 1
  fi
  sleep 0.1
done
if ! grep -Fq 'migration lock acquired' "$lock_output"; then
  echo 'migration lock holder did not acquire its lock in time' >&2
  exit 1
fi

set +e
lock_rejection=$(docker compose -f "$compose_file" run --rm -e MIGRATION_LOCK_TIMEOUT_SECONDS=0 migrate 2>&1)
lock_rejection_status=$?
set -e
if [[ $lock_rejection_status -eq 0 ]]; then
  echo 'expected concurrent migration job to be rejected by the migration lock' >&2
  exit 1
fi
if ! grep -Fq 'migration lock unavailable after 0s' <<<"$lock_rejection"; then
  echo 'concurrent migration lock rejection lacked the expected diagnostic' >&2
  printf '%s\n' "$lock_rejection" >&2
  exit 1
fi
wait "$holder_pid"
holder_pid=''

cp -a "$migrations_dir/." "$fixture_dir"
first_migration=$(find "$fixture_dir" -maxdepth 1 -type f -name '*.sql' | sort | head -n 1)
printf '\n-- checksum-mismatch regression fixture\n' >> "$first_migration"

set +e
output=$(docker compose -f "$compose_file" run --rm -v "$fixture_dir:/migrations:ro" migrate 2>&1)
status=$?
set -e
if [[ $status -eq 0 ]]; then
  echo "expected migration checksum mismatch to fail" >&2
  exit 1
fi
if ! grep -Fq "migration checksum mismatch: $(basename "$first_migration")" <<<"$output"; then
  echo "migration checksum mismatch failed without the expected diagnostic" >&2
  printf '%s\n' "$output" >&2
  exit 1
fi

printf 'migration ledger regression passed\n'
