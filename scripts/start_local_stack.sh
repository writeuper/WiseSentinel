#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FILE="$ROOT_DIR/manifest/docker/docker-compose.yml"
WAIT_SECONDS=${WAIT_SECONDS:-90}
docker compose -f "$COMPOSE_FILE" up -d migrate platform prometheus
deadline=$((SECONDS + WAIT_SECONDS))
while (( SECONDS < deadline )); do
  if curl -fsS "http://127.0.0.1:8090/health/live" >/dev/null 2>&1; then break; fi
  sleep 1
done
if ! curl -fsS "http://127.0.0.1:8090/health/live" >/dev/null; then
  echo "platform live probe failed" >&2
  docker compose -f "$COMPOSE_FILE" ps
  exit 1
fi
curl -sS "http://127.0.0.1:8090/health/ready" || true
printf '\nplatform live at http://127.0.0.1:8090\n'
