#!/usr/bin/env bash
set -euo pipefail

# Runs only when real provider credentials and independently verified RAG Gold
# labels are present. It deliberately fails before deployment/API calls when
# either prerequisite is missing, so local smoke evidence cannot be promoted
# to a model-quality benchmark.
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FILE="$ROOT_DIR/manifest/docker/docker-compose.yml"
INPUT=${EVAL_INPUT:-$ROOT_DIR/docs/整理与提升/enterprise_agent_eval_cases_130.csv}
RUNS=${EVAL_RUNS:-3}
MIN_VERIFIED=${EVAL_MIN_VERIFIED_RAG:-100}
BASE_URL=${EVAL_BASE_URL:-http://127.0.0.1:8090/api/v1}
USERNAME=${EVAL_USERNAME:-sre@example.com}
PASSWORD=${EVAL_PASSWORD:-dev123}
OUTPUT_DIR=${EVAL_OUTPUT_DIR:-/tmp/wisesentinel-real-eval-$(date -u +%Y%m%dT%H%M%SZ)}

for name in LLM_API_KEY LLM_BASE_URL LLM_MODEL EMBED_API_KEY; do
  if [[ -z "${!name:-}" ]]; then
    echo "real evaluation prerequisite missing: $name" >&2
    exit 2
  fi
done
if ! [[ "$RUNS" =~ ^[1-9][0-9]*$ ]]; then
  echo "EVAL_RUNS must be a positive integer" >&2
  exit 2
fi
if ! [[ "$MIN_VERIFIED" =~ ^[1-9][0-9]*$ ]]; then
  echo "EVAL_MIN_VERIFIED_RAG must be a positive integer" >&2
  exit 2
fi

# This guard runs before stack start and before any provider request.
python3 "$ROOT_DIR/scripts/run_agent_eval.py" \
  --input "$INPUT" \
  --require-verified-relevance \
  --min-verified-relevance-samples "$MIN_VERIFIED" \
  --limit 0 \
  --output /tmp/wisesentinel-real-eval-preflight.csv \
  --summary-json /tmp/wisesentinel-real-eval-preflight.json \
  --allow-failures >/dev/null

mkdir -p "$OUTPUT_DIR"
LLM_API_KEY="$LLM_API_KEY" LLM_BASE_URL="$LLM_BASE_URL" \
LLM_MODEL="$LLM_MODEL" EMBED_API_KEY="$EMBED_API_KEY" \
docker compose -f "$COMPOSE_FILE" up -d --force-recreate platform

deadline=$((SECONDS + ${EVAL_READY_WAIT_SECONDS:-90}))
until curl -fsS "${BASE_URL%/api/v1}/health/ready" >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    echo "platform did not become ready for real evaluation" >&2
    exit 1
  fi
  sleep 1
done

token_json=$(curl -fsS -X POST "$BASE_URL/auth/token" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")
token=$(printf '%s' "$token_json" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
if [[ -z "$token" ]]; then
  echo "development login did not return an access token" >&2
  exit 1
fi

for ((run = 1; run <= RUNS; run++)); do
  python3 "$ROOT_DIR/scripts/run_agent_eval.py" \
    --base-url "$BASE_URL" \
    --bearer-token "$token" \
    --input "$INPUT" \
    --environment "${EVAL_ENVIRONMENT:-integration}" \
    --run-label "real-provider-run-$run" \
    --model-profile "${EVAL_MODEL_PROFILE:-$LLM_MODEL}" \
    --embedding-profile "${EVAL_EMBEDDING_PROFILE:-configured}" \
    --require-verified-relevance \
    --min-verified-relevance-samples "$MIN_VERIFIED" \
    --output "$OUTPUT_DIR/run-$run.csv" \
    --summary-json "$OUTPUT_DIR/run-$run-summary.json"
done

printf 'real evaluation artifacts: %s\n' "$OUTPUT_DIR"
