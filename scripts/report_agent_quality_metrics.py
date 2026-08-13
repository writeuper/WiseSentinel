#!/usr/bin/env python3
"""Emit privacy-safe Agent trace/tool and RAG latency aggregates.

The script deliberately reports aggregates only: no query, trace ID, tenant ID,
tool payload, document text, DSN, or model credential is printed.
It works with the local Docker MySQL service without requiring a host mysql CLI.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.request
from collections import defaultdict
from datetime import datetime, timezone
from typing import Any


def mysql_query(sql: str) -> list[list[str]]:
    """Run a read-only aggregate query through the configured MySQL container."""
    container = os.environ.get("MYSQL_CONTAINER", "wisesentinel-mysql")
    user = os.environ.get("MYSQL_USER", "ws")
    database = os.environ.get("MYSQL_DATABASE", "wisesentinel")
    # Password is passed via MYSQL_PWD inside the container process and never in
    # argv/output. Deployments should provide a secret-backed MYSQL_PWD.
    password = os.environ.get("MYSQL_PASSWORD", "ws123")
    cmd = ["docker", "exec", "-e", "MYSQL_PWD=" + password, container,
           "mysql", "--batch", "--skip-column-names", "--raw", "-u", user, database,
           "-e", sql]
    try:
        out = subprocess.run(cmd, check=True, capture_output=True, text=True,
                             timeout=15).stdout
    except (OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
        raise RuntimeError("database aggregate unavailable") from exc
    return [line.split("\t") for line in out.splitlines() if line.strip()]


def safe_int(value: str | None) -> int:
    try:
        return int(value or 0)
    except ValueError:
        return 0


def safe_float(value: str | None) -> float:
    try:
        return float(value or 0)
    except ValueError:
        return 0.0


def parse_prometheus(text: str) -> dict[str, Any]:
    """Calculate success/error-aware P95 from cumulative histogram samples.

    A histogram with a sparse bucket layout can legitimately reach P95 only at
    ``+Inf``. In that case the reported P95 is ``None`` rather than pretending
    the last finite bucket is an exact percentile.
    """
    buckets_by_outcome: dict[str, dict[float, float]] = defaultdict(lambda: defaultdict(float))
    sums: dict[str, float] = defaultdict(float)
    totals: dict[str, float] = defaultdict(float)
    for line in text.splitlines():
        if line.startswith("#") or "ws_rag_retrieval_duration_seconds" not in line:
            continue
        outcome_match = re.search(r'outcome="([^"]+)"', line)
        outcome = outcome_match.group(1) if outcome_match else "unknown"
        if "_bucket{" in line:
            match = re.search(r'le="([^"]+)"[^ ]*\s+([0-9.eE+-]+)$', line)
            if match:
                bound = float("inf") if match.group(1) == "+Inf" else float(match.group(1))
                buckets_by_outcome[outcome][bound] += safe_float(match.group(2))
        elif "_count" in line:
            totals[outcome] += safe_float(line.rsplit(" ", 1)[-1])
        elif "_sum" in line:
            sums[outcome] += safe_float(line.rsplit(" ", 1)[-1])

    def percentile(outcome: str) -> float | None:
        total = totals.get(outcome, 0.0)
        buckets = buckets_by_outcome.get(outcome, {})
        if not total or not buckets:
            return None
        target = total * 0.95
        bound = next((item for item, value in sorted(buckets.items()) if value >= target), None)
        return None if bound is None or bound == float("inf") else round(bound * 1000, 3)

    sample_count = int(sum(totals.values()))
    success_count = int(totals.get("success", 0.0))
    error_count = int(totals.get("error", 0.0))
    return {
        "sample_count": sample_count,
        "success_sample_count": success_count,
        "error_sample_count": error_count,
        "p95_ms": percentile("success"),
        "success_p95_ms": percentile("success"),
        "all_p95_ms": None if sample_count == 0 else percentile("success") if error_count == 0 else None,
        "mean_ms": round(sum(sums.values()) / sample_count * 1000, 3) if sample_count else None,
        "success_mean_ms": round(sums.get("success", 0.0) / success_count * 1000, 3) if success_count else None,
        "p95_semantics": "histogram_bucket_upper_bound_ms" if sample_count else None,
    }


def parse_model_prometheus(text: str) -> dict[str, Any]:
    """Aggregate model generation latency without exposing provider details.

    Percentiles are finite histogram bucket upper bounds.  Keep success and
    timeout outcomes separate so upstream model tail latency is not confused
    with RAG or end-to-end Agent latency.
    """
    buckets: dict[str, dict[float, float]] = defaultdict(lambda: defaultdict(float))
    sums: dict[str, float] = defaultdict(float)
    totals: dict[str, float] = defaultdict(float)
    for line in text.splitlines():
        if line.startswith("#") or "ws_model_call_duration_seconds" not in line:
            continue
        outcome_match = re.search(r'outcome="([^"]+)"', line)
        outcome = outcome_match.group(1) if outcome_match else "unknown"
        if "_bucket{" in line:
            match = re.search(r'le="([^"]+)"[^ ]*\s+([0-9.eE+-]+)$', line)
            if match:
                bound = float("inf") if match.group(1) == "+Inf" else float(match.group(1))
                buckets[outcome][bound] += safe_float(match.group(2))
        elif "_count" in line:
            totals[outcome] += safe_float(line.rsplit(" ", 1)[-1])
        elif "_sum" in line:
            sums[outcome] += safe_float(line.rsplit(" ", 1)[-1])

    def p95(outcome: str) -> float | None:
        total = totals.get(outcome, 0.0)
        if not total:
            return None
        target = total * 0.95
        bound = next((item for item, value in sorted(buckets.get(outcome, {}).items()) if value >= target), None)
        return None if bound is None or bound == float("inf") else round(bound * 1000, 3)

    success = int(totals.get("success", 0.0))
    timeout = int(totals.get("timeout", 0.0))
    return {
        "sample_count": int(sum(totals.values())),
        "success_sample_count": success,
        "timeout_sample_count": timeout,
        "success_p95_ms": p95("success"),
        "success_mean_ms": round(sums.get("success", 0.0) / success * 1000, 3) if success else None,
        "p95_semantics": "histogram_bucket_upper_bound_ms" if totals else None,
    }


def fetch_raw_metrics(url: str) -> str:
    try:
        with urllib.request.urlopen(url, timeout=10) as response:
            return response.read().decode("utf-8", "replace")
    except Exception:
        return ""


def fetch_metrics(url: str) -> dict[str, Any]:
    text = fetch_raw_metrics(url)
    if text:
        return parse_prometheus(text)
    else:
        return {"sample_count": 0, "success_sample_count": 0, "error_sample_count": 0,
                "p95_ms": None, "success_p95_ms": None, "all_p95_ms": None,
                "mean_ms": None, "success_mean_ms": None, "p95_semantics": None}


def aggregate() -> dict[str, Any]:
    # A non-null finished_at is the durable terminal marker. Treat every
    # finished non-success status as terminal failure so adding a new terminal
    # state cannot silently drop it from the latency denominator.
    traces = mysql_query("""
      SELECT COUNT(*), COALESCE(SUM(CASE WHEN status IN ('success','completed') THEN 1 ELSE 0 END),0),
             COALESCE(SUM(CASE WHEN status NOT IN ('success','completed') THEN 1 ELSE 0 END),0),
             COALESCE(AVG(CASE WHEN status IN ('success','completed') THEN latency_ms END),0),
             COALESCE(AVG(CASE WHEN status NOT IN ('success','completed') THEN latency_ms END),0)
      FROM ws_agent_trace WHERE finished_at IS NOT NULL
    """)[0]
    latency_by_agent = mysql_query("""
      SELECT agent_type, COUNT(*),
             COALESCE(AVG(CASE WHEN status IN ('success','completed') THEN latency_ms END),0),
             COALESCE(AVG(CASE WHEN status NOT IN ('success','completed') THEN latency_ms END),0),
             COALESCE(AVG(latency_ms),0)
      FROM ws_agent_trace WHERE finished_at IS NOT NULL
      GROUP BY agent_type ORDER BY agent_type
    """)
    # The denominator is every finished Agent trace, including a terminal
    # trace that has no persisted step (for example an early validation or
    # admission failure).  Joining only ws_agent_trace_step would both exclude
    # those zero-step traces and accidentally include unfinished traces.
    steps = mysql_query("""
      SELECT COUNT(*), COALESCE(AVG(step_count),0), COALESCE(MIN(step_count),0), COALESCE(MAX(step_count),0)
      FROM (
        SELECT t.trace_id, COUNT(s.id) step_count
        FROM ws_agent_trace t
        LEFT JOIN ws_agent_trace_step s ON s.trace_id = t.trace_id AND s.tenant_id = t.tenant_id
        WHERE t.finished_at IS NOT NULL
        GROUP BY t.trace_id
      ) s
    """)[0]
    by_agent = mysql_query("""
      SELECT agent_type, COUNT(*), COALESCE(AVG(step_count),0)
      FROM (SELECT t.agent_type, t.trace_id, COUNT(s.id) step_count
            FROM ws_agent_trace t LEFT JOIN ws_agent_trace_step s ON s.trace_id=t.trace_id AND s.tenant_id=t.tenant_id
            WHERE t.finished_at IS NOT NULL GROUP BY t.agent_type,t.trace_id) x
      GROUP BY agent_type ORDER BY agent_type
    """)
    tools = mysql_query("""
      SELECT COUNT(*), COALESCE(SUM(CASE WHEN status IN ('success','succeeded','completed') THEN 1 ELSE 0 END),0)
      FROM ws_tool_call_record
    """)[0]
    metrics_text = fetch_raw_metrics(os.environ.get("METRICS_URL", "http://127.0.0.1:8090/metrics"))
    result: dict[str, Any] = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "trace": {"completed_or_success": safe_int(traces[1]), "failed_or_terminal_error": safe_int(traces[2]),
                   "finished": safe_int(traces[0]), "avg_latency_ms_success": round(safe_float(traces[3]), 2),
                   "avg_latency_ms_failed": round(safe_float(traces[4]), 2),
                   "avg_latency_ms_all": round((safe_float(traces[3]) * safe_int(traces[1]) + safe_float(traces[4]) * safe_int(traces[2])) / max(1, safe_int(traces[1]) + safe_int(traces[2])), 2)},
        "steps": {"finished_traces": safe_int(steps[0]), "traces_with_steps": safe_int(steps[0]), "avg_per_trace": round(safe_float(steps[1]), 3),
                   "min": safe_int(steps[2]), "max": safe_int(steps[3])},
        "by_agent_type": [{"agent_type": row[0], "traces": safe_int(row[1]), "avg_steps": round(safe_float(row[2]), 3)} for row in by_agent],
        "latency_by_agent_type": [{"agent_type": row[0], "traces": safe_int(row[1]),
                                    "avg_latency_ms_success": round(safe_float(row[2]), 2),
                                    "avg_latency_ms_failed": round(safe_float(row[3]), 2),
                                    "avg_latency_ms_all": round(safe_float(row[4]), 2)} for row in latency_by_agent],
        "tool_calls": {"total": safe_int(tools[0]), "successful": safe_int(tools[1]),
                        "success_rate": round(safe_int(tools[1]) / safe_int(tools[0]) * 100, 2) if safe_int(tools[0]) else None},
        "rag_retrieval": parse_prometheus(metrics_text) if metrics_text else fetch_metrics(os.environ.get("METRICS_URL", "http://127.0.0.1:8090/metrics")),
        "model_generation": parse_model_prometheus(metrics_text),
    }
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    args = parser.parse_args()
    try:
        report = aggregate()
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    if args.format == "json":
        print(json.dumps(report, ensure_ascii=False, indent=2))
    else:
        print("# Agent Quality Aggregate Report\n")
        print(f"- Generated (UTC): {report['generated_at']}")
        print(f"- Finished traces: {report['trace']['finished']}")
        print(f"- Average Agent latency (success): {report['trace']['avg_latency_ms_success']} ms")
        print(f"- Average Agent latency (all finished): {report['trace']['avg_latency_ms_all']} ms")
        print(f"- Average Agent steps/trace: {report['steps']['avg_per_trace']}")
        print(f"- Tool success rate: {report['tool_calls']['success_rate'] if report['tool_calls']['success_rate'] is not None else 'N/A'}%")
        rag = report["rag_retrieval"]
        print(f"- RAG retrieval samples: {rag['sample_count']}")
        print(f"- RAG successful samples: {rag.get('success_sample_count', 0)}")
        print(f"- RAG error samples: {rag.get('error_sample_count', 0)}")
        print(f"- RAG successful retrieval P95: {rag.get('success_p95_ms') if rag.get('success_p95_ms') is not None else 'N/A'} ms")
        print(f"- RAG successful retrieval mean: {rag.get('success_mean_ms') if rag.get('success_mean_ms') is not None else 'N/A'} ms")
        if rag.get("p95_semantics"):
            print("- RAG P95 semantics: histogram bucket upper bound (not an exact percentile)")
        model = report["model_generation"]
        print(f"- Model generation samples: {model['sample_count']}")
        print(f"- Model generation success samples: {model['success_sample_count']}")
        print(f"- Model generation timeout samples: {model['timeout_sample_count']}")
        print(f"- Model generation successful P95: {model['success_p95_ms'] if model['success_p95_ms'] is not None else 'N/A'} ms")
        print(f"- Model generation successful mean: {model['success_mean_ms'] if model['success_mean_ms'] is not None else 'N/A'} ms")
        if rag["sample_count"] < 30 or rag.get("success_sample_count", 0) < 30:
            print("\n> RAG P95 is provisional when fewer than 30 total and successful retrieval samples exist; this is not a production SLA.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
