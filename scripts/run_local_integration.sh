#!/usr/bin/env bash
set -euo pipefail

# Uses only the disposable local Compose dependencies. The repository's
# integration tests create and clean up uniquely named test records.
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
MYSQL_DSN=${OPS_TEST_MYSQL_DSN:-mysql:ws:ws123@tcp(127.0.0.1:3306)/wisesentinel?charset=utf8mb4\&parseTime=True\&loc=Local}
REDIS_ADDR=${OPS_TEST_REDIS_ADDR:-127.0.0.1:6379}

bash "$ROOT_DIR/scripts/start_local_stack.sh" >/tmp/wisesentinel-start-local-stack.log
OPS_TEST_MYSQL_DSN="$MYSQL_DSN" \
OPS_TEST_REDIS_ADDR="$REDIS_ADDR" \
RAG_TEST_TIMEOUT="${RAG_TEST_TIMEOUT:-3m}" \
make -C "$ROOT_DIR" test-real-scenarios
