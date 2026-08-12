#!/usr/bin/env bash
set -euo pipefail

# Catch direct writes of known untrusted values to GoFrame logs. This is a
# deliberately small, high-signal guardrail; runtime redaction tests remain
# the authority for semantic coverage.
readonly SOURCES=(internal/toolkit internal/agent internal/rag internal/gateway)
readonly PATTERN='g\.Log\(\)\.(Infof|Warningf|Errorf)\([^\n]*(req\.Input|argumentsInJSON|req\.Query|err\.Error\(\))'

matches="$(rg -n -e "$PATTERN" "${SOURCES[@]}" || true)"
unsafe="$(printf '%s\n' "$matches" | rg -v 'diagnosticSummary\(|redact\.(Summary|TelemetryProjection)\(' || true)"

if [[ -n "$unsafe" ]]; then
  echo "Unsafe sensitive value sent directly to a log sink:" >&2
  printf '%s\n' "$unsafe" >&2
  echo "Wrap diagnostic values with redact.Summary, redact.TelemetryProjection, or diagnosticSummary." >&2
  exit 1
fi

echo "Sensitive-sink static guard passed."
