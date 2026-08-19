#!/usr/bin/env bash
set -euo pipefail

# Local-only evaluation orchestration. This intentionally uses the development
# login and keeps infrastructure failures visible in the summary; it does not
# turn an unconfigured model provider into a quality pass.
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BASE_URL=${EVAL_BASE_URL:-http://127.0.0.1:8090/api/v1}
USERNAME=${EVAL_USERNAME:-sre@example.com}
PASSWORD=${EVAL_PASSWORD:-dev123}
# TIME-008 exercises authenticated HTTP, session cleanup, a successful local
# MCP tool call, forbidden-tool checks and Trace retrieval without claiming a
# real LLM benchmark. Callers can select another case explicitly.
CASE_ID=${EVAL_CASE:-TIME-008}
OUTPUT=${EVAL_OUTPUT:-/tmp/wisesentinel-local-eval.csv}
SUMMARY=${EVAL_SUMMARY:-/tmp/wisesentinel-local-eval-summary.json}

bash "$ROOT_DIR/scripts/start_local_stack.sh" >/tmp/wisesentinel-start-local-stack.log

token_json=$(curl -fsS -X POST "$BASE_URL/auth/token" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")
token=$(printf '%s' "$token_json" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
if [[ -z "$token" ]]; then
  echo "development login did not return an access token" >&2
  exit 1
fi

python3 "$ROOT_DIR/scripts/run_agent_eval.py" \
  --base-url "$BASE_URL" \
  --bearer-token "$token" \
  --only chat \
  --case "$CASE_ID" \
  --allow-failures \
  --environment local \
  --run-label local-start-eval \
  --model-profile "${EVAL_MODEL_PROFILE:-local-config}" \
  --embedding-profile "${EVAL_EMBEDDING_PROFILE:-local-config}" \
  --output "$OUTPUT" \
  --summary-json "$SUMMARY"

printf 'local evaluation summary: %s\n' "$SUMMARY"
