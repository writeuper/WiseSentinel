#!/bin/sh
set -eu

mysql_host=${MYSQL_HOST:-mysql}
mysql_port=${MYSQL_PORT:-3306}
mysql_user=${MYSQL_USER:-ws}
mysql_database=${MYSQL_DATABASE:-wisesentinel}
lock_name='wisesentinel_schema_migration'
lock_timeout=${MIGRATION_LOCK_TIMEOUT_SECONDS:-30}
lock_hold=${MIGRATION_LOCK_HOLD_SECONDS:-0}
lock_pid=''
holder_token="migration-$(date +%s%N)-$$"

case "$lock_timeout" in
  ''|*[!0-9]*) echo 'MIGRATION_LOCK_TIMEOUT_SECONDS must be a non-negative integer' >&2; exit 2 ;;
esac
case "$lock_hold" in
  ''|*[!0-9]*) echo 'MIGRATION_LOCK_HOLD_SECONDS must be a non-negative integer' >&2; exit 2 ;;
esac

cleanup() {
  if [ -n "$lock_pid" ] && kill -0 "$lock_pid" 2>/dev/null; then
    kill "$lock_pid" 2>/dev/null || true
  fi
  if [ -n "$lock_pid" ]; then
    wait "$lock_pid" 2>/dev/null || true
  fi
}
trap cleanup 0 1 2 15

mysql -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
  'CREATE TABLE IF NOT EXISTS ws_schema_migration (migration_name VARCHAR(255) NOT NULL, checksum CHAR(64) NOT NULL, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (migration_name)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4'
mysql -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
  'CREATE TABLE IF NOT EXISTS ws_schema_migration_lock (lock_name VARCHAR(128) NOT NULL, holder_token VARCHAR(128) NOT NULL, acquired_at DATETIME NOT NULL, PRIMARY KEY (lock_name)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4'

# A database that records a migration absent from this artifact is newer than
# the code being deployed (or its migration history was removed). Refuse that
# downgrade before taking the execution lock or starting the platform.
applied_migrations=$(mysql -N -B -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
  'SELECT migration_name FROM ws_schema_migration ORDER BY migration_name')
missing_migration=''
while IFS= read -r applied_migration; do
  [ -z "$applied_migration" ] && continue
  if [ ! -f "/migrations/$applied_migration" ]; then
    missing_migration=$applied_migration
    break
  fi
done <<EOF
$applied_migrations
EOF
if [ -n "$missing_migration" ]; then
  echo "migration history is not present in deployment artifact: $missing_migration" >&2
  exit 1
fi

# GET_LOCK is session-scoped. Keep this client session alive for the complete
# migration run so independently started Compose jobs cannot interleave DDL.
# The marker row is a handshake only: the advisory lock remains the authority.
mysql -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
  "SET @ws_lock_acquired := GET_LOCK('$lock_name', $lock_timeout); INSERT INTO ws_schema_migration_lock (lock_name, holder_token, acquired_at) SELECT '$lock_name', '$holder_token', NOW() WHERE @ws_lock_acquired = 1 ON DUPLICATE KEY UPDATE holder_token = VALUES(holder_token), acquired_at = VALUES(acquired_at); DO SLEEP(IF(@ws_lock_acquired = 1, 86400, 0))" >/dev/null &
lock_pid=$!

attempt=0
max_attempts=$(( (lock_timeout + 5) * 10 ))
while :; do
  observed_token=$(mysql -N -B -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
    "SELECT holder_token FROM ws_schema_migration_lock WHERE lock_name = '$lock_name'")
  if [ "$observed_token" = "$holder_token" ]; then
    break
  fi
  if ! kill -0 "$lock_pid" 2>/dev/null; then
    wait "$lock_pid"
    echo "migration lock unavailable after ${lock_timeout}s" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge "$max_attempts" ]; then
    echo "migration lock acquisition did not return within $((lock_timeout + 5))s" >&2
    exit 1
  fi
  sleep 0.1
done
echo 'migration lock acquired'

if [ "$lock_hold" -gt 0 ]; then
  sleep "$lock_hold"
fi

for file in /migrations/*.sql; do
  if ! kill -0 "$lock_pid" 2>/dev/null; then
    echo 'migration lock holder stopped unexpectedly' >&2
    exit 1
  fi
  migration_name=$(basename "$file")
  checksum=$(sha256sum "$file" | awk '{print $1}')
  existing_checksum=$(mysql -N -B -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
    "SELECT checksum FROM ws_schema_migration WHERE migration_name = '$migration_name'")
  if [ -n "$existing_checksum" ]; then
    if [ "$existing_checksum" != "$checksum" ]; then
      echo "migration checksum mismatch: $migration_name" >&2
      exit 1
    fi
    echo "Migration $migration_name already applied; checksum verified"
    continue
  fi
  echo "Applying migration $migration_name"
  mysql -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" <"$file"
  mysql -h "$mysql_host" -P "$mysql_port" -u "$mysql_user" "$mysql_database" -e \
    "INSERT INTO ws_schema_migration (migration_name, checksum) VALUES ('$migration_name', '$checksum')"
done
