#!/usr/bin/env bash
set -Eeuo pipefail

PLATFORM_URL="${PLATFORM_URL:-http://127.0.0.1:8090}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
PROBE_PATH="${PROBE_PATH:-/health/live}"
METRIC="${METRIC:-ws_http_requests_total}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-2}"
MAX_ATTEMPTS="${MAX_ATTEMPTS:-30}"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    log "缺少命令：$1"
    exit 1
  }
}

query_prometheus() {
  curl -fsS --get "$PROMETHEUS_URL/api/v1/query" \
    --data-urlencode "query=$1"
}

has_metric_result() {
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
if payload.get("status") != "success":
    raise SystemExit(1)
if payload.get("data", {}).get("result"):
    raise SystemExit(0)
raise SystemExit(1)
'
}

require_command curl
require_command python3

log "检查 WiseSentinel：$PLATFORM_URL$PROBE_PATH"
curl -fsS "$PLATFORM_URL$PROBE_PATH" >/dev/null
log "平台接口访问成功"

log "生成一次 HTTP 指标：$METRIC"
curl -fsS "$PLATFORM_URL$PROBE_PATH" >/dev/null

log "检查 Prometheus target"
targets=$(curl -fsS "$PROMETHEUS_URL/api/v1/targets")
python3 -c '
import json
import sys

items = json.loads(sys.argv[1])["data"]["activeTargets"]
platform = [x for x in items if x["labels"].get("job") == "wisesentinel-platform"]
if not platform:
    print("未找到 wisesentinel-platform target", file=sys.stderr)
    raise SystemExit(1)
for item in platform:
    print("job={} health={} url={} error={}".format(
        item["labels"].get("job"),
        item["health"],
        item["scrapeUrl"],
        item.get("lastError", ""),
    ))
if any(item["health"] != "up" or item.get("lastError") for item in platform):
    raise SystemExit(1)
' "$targets"

log "轮询 Prometheus 指标入库：$METRIC"
for ((attempt = 1; attempt <= MAX_ATTEMPTS; attempt++)); do
  response=$(query_prometheus "$METRIC" || true)
  if printf '%s' "$response" | has_metric_result; then
    log "验证成功：Prometheus 已采集 $METRIC（attempt=$attempt）"
    printf '%s\n' "$response" | python3 -m json.tool
    exit 0
  fi

  log "尚未查询到指标（attempt=$attempt/$MAX_ATTEMPTS），${INTERVAL_SECONDS}s 后重试"
  sleep "$INTERVAL_SECONDS"
done

log "验证失败：在 ${MAX_ATTEMPTS} 次轮询内未查询到 $METRIC"
log "请检查 Prometheus target、抓取间隔和平台 /metrics 输出"
exit 1
