#!/usr/bin/env python3
"""Emit privacy-safe Agent trace/tool and RAG latency aggregates.

The script deliberately reports aggregates only: no query, trace ID, tenant ID,
tool payload, document text, DSN, or model credential is printed.
It works with the local Docker MySQL service without requiring a host mysql CLI.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import subprocess
import sys
import urllib.request
from collections import defaultdict
from datetime import datetime, timezone
from typing import Any

TRAFFIC_STATUSES = {"synthetic", "staging", "production_attested"}
TRAFFIC_SOURCES = {"gateway_aggregate", "analytics_aggregate", "load_test"}
TOOL_OUTCOMES = {"success", "error", "rejected", "unavailable", "timeout"}


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


def percentile_ms(values: list[float], percentile: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, int((len(ordered) * percentile + 0.999999999) - 1)))
    return round(ordered[index], 3)


def normalize_tool_outcome(status: str) -> str:
    value = str(status or "").strip().lower()
    if value in {"succeeded", "completed"}:
        return "success"
    return value if value in TOOL_OUTCOMES else "other"


def aggregate_tool_latency(rows: list[list[str]]) -> dict[str, Any]:
    """Aggregate tool timings and dependency outcomes without payloads."""
    grouped: dict[str, list[tuple[str, float]]] = defaultdict(list)
    for row in rows:
        if len(row) < 3:
            continue
        name = str(row[0] or "unknown")
        status = normalize_tool_outcome(row[1])
        try:
            latency = float(row[2] or 0)
        except ValueError:
            continue
        if latency < 0:
            continue
        grouped[name].append((status, latency))

    result = []
    for name in sorted(grouped):
        samples = grouped[name]
        all_values = [latency for _, latency in samples]
        outcome_counts = {outcome: sum(1 for status, _ in samples if status == outcome) for outcome in sorted(TOOL_OUTCOMES | {"other"})}
        success_values = [latency for status, latency in samples if status == "success"]
        attempted = sum(outcome_counts[outcome] for outcome in ("success", "error", "unavailable", "timeout"))
        result.append({
            "tool_name": name,
            "calls": len(samples),
            "successes": len(success_values),
            "success_rate": round(len(success_values) / len(samples) * 100, 2) if samples else None,
            "outcomes": outcome_counts,
            "dependency_attempts": attempted,
            "dependency_availability_rate": round(outcome_counts["success"] / attempted * 100, 2) if attempted else None,
            "dependency_response_rate": round((outcome_counts["success"] + outcome_counts["error"]) / attempted * 100, 2) if attempted else None,
            "unavailable_rate": round(outcome_counts["unavailable"] / attempted * 100, 2) if attempted else None,
            "timeout_rate": round(outcome_counts["timeout"] / attempted * 100, 2) if attempted else None,
            "mean_ms": round(sum(all_values) / len(all_values), 3) if all_values else None,
            "zero_latency_samples": sum(1 for latency in all_values if latency == 0),
            "zero_latency_rate": round(sum(1 for latency in all_values if latency == 0) / len(all_values) * 100, 2) if all_values else None,
            "p50_ms": percentile_ms(all_values, 0.50),
            "p95_ms": percentile_ms(all_values, 0.95),
            "success_p95_ms": percentile_ms(success_values, 0.95),
        })
    attempted = sum(item["dependency_attempts"] for item in result)
    available = sum(item["outcomes"]["success"] for item in result)
    responded = sum(item["outcomes"]["success"] + item["outcomes"]["error"] for item in result)
    return {
        "tool_count": len(result),
        "by_tool": result,
        "dependency_attempts": attempted,
        "dependency_available": available,
        "dependency_availability_rate": round(available / attempted * 100, 2) if attempted else None,
        "dependency_responded": responded,
        "dependency_response_rate": round(responded / attempted * 100, 2) if attempted else None,
    }


def aggregate_index_tasks(rows: list[list[str]]) -> dict[str, Any]:
    """Aggregate durable index-task statuses without exposing task/document IDs."""
    counts: dict[str, int] = defaultdict(int)
    error_categories: dict[str, int] = defaultdict(int)
    for row in rows:
        if not row:
            continue
        status = str(row[0] or "unknown")
        count = safe_int(row[-1]) if len(row) > 1 else 1
        counts[status] += count
        if status in {"failed", "dead"}:
            error = " ".join(str(value or "") for value in row[1:-1]).lower()
            if "deadlineexceeded" in error or "deadline exceeded" in error:
                error_categories["milvus_deadline"] += count
            elif "document deleted" in error:
                error_categories["document_deleted"] += count
            elif "embed http 403" in error or "quota" in error or "free tier" in error:
                error_categories["embedding_quota"] += count
            elif "embed http 404" in error or "not found" in error:
                error_categories["embedding_not_found"] += count
            elif error:
                error_categories["other"] += count
    total = sum(counts.values())
    failed = sum(value for status, value in counts.items() if status in {"failed", "dead"})
    retryable = sum(value for status, value in counts.items() if status in {"pending", "retry_wait", "running"})
    return {
        "total": total,
        "by_status": dict(sorted(counts.items())),
        "failed_or_dead": failed,
        "retryable_or_running": retryable,
        "failure_rate": round(failed / total * 100, 2) if total else None,
        "failure_categories": dict(sorted(error_categories.items())),
    }


OPS_TASK_STATUSES = {"pending", "running", "retrying", "timeout", "success", "failed", "awaiting_approval"}

APPROVAL_STATUSES = {"pending", "approved", "rejected", "expired", "canceled"}


def normalize_ops_task_status(status: str) -> str:
    value = str(status or "").strip()
    return value if value in OPS_TASK_STATUSES else "other"


def aggregate_ops_tasks(rows: list[list[str]]) -> dict[str, Any]:
    """Aggregate durable Ops task states and e2e latency without identifiers.

    Rows are ``status, total, finished_count, latency_sum_ms``.  The query
    deliberately supplies counts/sums rather than task IDs or input bodies.
    """
    by_status: dict[str, int] = defaultdict(int)
    finished = success = failed = in_flight = 0
    latency_sum = 0.0
    for row in rows:
        if len(row) < 2:
            continue
        status = normalize_ops_task_status(row[0])
        total = max(0, safe_int(row[1]))
        finished_count = max(0, safe_int(row[2])) if len(row) > 2 else 0
        latency = max(0.0, safe_float(row[3])) if len(row) > 3 else 0.0
        by_status[status] += total
        finished += finished_count
        latency_sum += latency
        if status == "success":
            success += finished_count
        elif status in {"failed", "timeout"}:
            failed += finished_count
        elif status in {"pending", "running", "retrying"}:
            in_flight += total
    return {
        "total": sum(by_status.values()),
        "finished": finished,
        "in_flight": in_flight,
        "successful": success,
        "failed_or_timeout": failed,
        "completion_rate": round(success / finished * 100, 2) if finished else None,
        "failure_rate": round(failed / finished * 100, 2) if finished else None,
        "avg_e2e_latency_ms": round(latency_sum / finished, 3) if finished else None,
        "by_status": dict(sorted(by_status.items())),
    }


def normalize_approval_status(status: str) -> str:
    value = str(status or "").strip().lower()
    return value if value in APPROVAL_STATUSES else "other"


def aggregate_approvals(rows: list[list[str]]) -> dict[str, Any]:
    """Aggregate approval decisions and wait latency without identifiers.

    Rows are ``status, decision_latency_ms``. Pending rows carry an empty
    latency. The query deliberately omits approval/task/user IDs and payloads.
    """
    by_status: dict[str, int] = defaultdict(int)
    decision_latencies: list[float] = []
    for row in rows:
        if not row:
            continue
        status = normalize_approval_status(row[0])
        by_status[status] += 1
        if status == "pending" or len(row) < 2 or not str(row[1] or "").strip():
            continue
        try:
            latency = float(row[1])
        except ValueError:
            continue
        if latency >= 0:
            decision_latencies.append(latency)
    total = sum(by_status.values())
    decided = total - by_status.get("pending", 0)
    return {
        "total": total,
        "decided": decided,
        "pending": by_status.get("pending", 0),
        "approved": by_status.get("approved", 0),
        "rejected": by_status.get("rejected", 0),
        "expired": by_status.get("expired", 0),
        "decision_rate": round(decided / total * 100, 2) if total else None,
        "by_status": dict(sorted(by_status.items())),
        "decision_latency_samples": len(decision_latencies),
        "decision_latency_mean_ms": round(sum(decision_latencies) / len(decision_latencies), 3) if decision_latencies else None,
        "decision_latency_p50_ms": percentile_ms(decision_latencies, 0.50),
        "decision_latency_p95_ms": percentile_ms(decision_latencies, 0.95),
    }


def aggregate_feedback(rows: list[list[str]]) -> dict[str, Any]:
    """Aggregate bounded user feedback without target/user IDs or comments."""
    by_target: dict[str, dict[str, int]] = defaultdict(lambda: {"useful": 0, "bad": 0, "other": 0})
    for row in rows:
        if len(row) < 2:
            continue
        target = str(row[0] or "other").strip().lower()
        if target not in {"fault_knowledge", "answer", "tool_call"}:
            target = "other"
        rating = str(row[1] or "other").strip().lower()
        if rating not in {"useful", "bad"}:
            rating = "other"
        by_target[target][rating] += 1
    useful = sum(values["useful"] for values in by_target.values())
    bad = sum(values["bad"] for values in by_target.values())
    other = sum(values["other"] for values in by_target.values())
    total = useful + bad + other
    rated = useful + bad
    return {
        "total": total,
        "rated": rated,
        "useful": useful,
        "bad": bad,
        "other": other,
        "useful_rate": round(useful / rated * 100, 2) if rated else None,
        "by_target": {target: dict(sorted(values.items())) for target, values in sorted(by_target.items())},
    }


def aggregate_trace_latency(rows: list[list[str]]) -> dict[str, Any]:
    """Aggregate durable Agent trace latency without exposing identifiers.

    ``abandoned`` is a recovery terminal state and is reported separately from
    business terminal outcomes. Invalid/negative samples are ignored rather
    than allowing corrupted telemetry to change a percentile.
    """
    grouped: dict[str, list[float]] = defaultdict(list)
    for row in rows:
        if len(row) < 2:
            continue
        status = str(row[0] or "unknown")
        try:
            latency = float(row[1] or 0)
        except ValueError:
            continue
        if latency < 0:
            continue
        grouped[status].append(latency)

    success = [v for status, values in grouped.items() if status in {"success", "completed"} for v in values]
    business = [v for status, values in grouped.items() if status not in {"abandoned"} for v in values]
    all_values = [v for values in grouped.values() for v in values]

    def summary(values: list[float]) -> dict[str, Any]:
        return {
            "samples": len(values),
            "mean_ms": round(sum(values) / len(values), 3) if values else None,
            "p50_ms": percentile_ms(values, 0.50),
            "p95_ms": percentile_ms(values, 0.95),
        }

    return {"success": summary(success), "business_terminal": summary(business), "all_finished": summary(all_values)}


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


def parse_rag_inventory(text: str) -> dict[str, int | None]:
    """Read aggregate RAG inventory gauges without tenant/document labels."""
    names = (
        "ws_rag_active_documents",
        "ws_rag_active_published_chunks",
        "ws_rag_active_legacy_documents",
        "ws_rag_physical_vectors",
    )
    values: dict[str, int | None] = {name: None for name in names}
    for line in text.splitlines():
        if line.startswith("#"):
            continue
        for name in names:
            if line.startswith(name + " "):
                raw = line.rsplit(" ", 1)[-1]
                try:
                    values[name] = int(float(raw))
                except ValueError:
                    pass
    return {
        "active_documents": values["ws_rag_active_documents"],
        "active_published_chunks": values["ws_rag_active_published_chunks"],
        "active_legacy_documents": values["ws_rag_active_legacy_documents"],
        "physical_vectors": values["ws_rag_physical_vectors"],
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


def parse_breaker_events(text: str) -> dict[str, int]:
    """Aggregate low-cardinality breaker transition events by event type."""
    events = {name: 0 for name in ("open", "rejected", "probe", "closed")}
    for line in text.splitlines():
        if line.startswith("#") or not line.startswith("ws_model_breaker_events_total{"):
            continue
        match = re.search(r'event="(open|rejected|probe|closed)"[^ ]*\s+([0-9.eE+-]+)$', line)
        if match:
            events[match.group(1)] += int(float(match.group(2)))
    events["total"] = sum(events.values())
    return events


def parse_model_admission(text: str) -> dict[str, Any]:
    """Aggregate model admission capacity signals without provider labels."""
    result: dict[str, Any] = {
        "rejections": 0,
        "in_flight": 0,
        "wait_accepted_count": 0,
        "wait_rejected_count": 0,
        "wait_accepted_seconds": 0.0,
        "wait_rejected_seconds": 0.0,
    }
    for line in text.splitlines():
        if line.startswith("#"):
            continue
        try:
            metric, raw = line.rsplit(" ", 1)
            value = float(raw)
        except (ValueError, TypeError):
            continue
        if metric.startswith("ws_model_admission_rejections_total{"):
            result["rejections"] += int(value)
        elif metric.startswith("ws_model_admission_in_flight{"):
            result["in_flight"] += int(value)
        elif metric.startswith("ws_model_admission_wait_seconds_count{"):
            if 'outcome="accepted"' in metric:
                result["wait_accepted_count"] += int(value)
            elif 'outcome="rejected"' in metric:
                result["wait_rejected_count"] += int(value)
        elif metric.startswith("ws_model_admission_wait_seconds_sum{"):
            if 'outcome="accepted"' in metric:
                result["wait_accepted_seconds"] += value
            elif 'outcome="rejected"' in metric:
                result["wait_rejected_seconds"] += value
    total_decisions = result["wait_accepted_count"] + result["wait_rejected_count"]
    result["rejection_rate"] = round(result["wait_rejected_count"] / total_decisions * 100, 2) if total_decisions else None
    result["mean_wait_accepted_ms"] = round(result["wait_accepted_seconds"] / result["wait_accepted_count"] * 1000, 3) if result["wait_accepted_count"] else None
    result["mean_wait_rejected_ms"] = round(result["wait_rejected_seconds"] / result["wait_rejected_count"] * 1000, 3) if result["wait_rejected_count"] else None
    return result


def parse_model_tokens(text: str) -> dict[str, Any]:
    """Aggregate bounded model token counters without provider/model labels."""
    totals = {name: 0 for name in ("prompt", "completion", "total")}
    operations: dict[str, int] = defaultdict(int)
    for line in text.splitlines():
        if line.startswith("#") or not line.startswith("ws_model_tokens_total{"):
            continue
        match = re.search(r'operation="(generate|stream_handshake|stream_complete)".*token_type="(prompt|completion|total)"[^ ]*\s+([0-9.eE+-]+)$', line)
        if not match:
            continue
        operation, token_type, raw = match.groups()
        value = int(float(raw))
        totals[token_type] += value
        operations[operation] += value
    return {
        "prompt_tokens": totals["prompt"],
        "completion_tokens": totals["completion"],
        "total_tokens": totals["total"],
        "operations": dict(sorted(operations.items())),
        "usage_samples": int(bool(sum(totals.values()))),
        "input_output_ratio": round(totals["prompt"] / totals["completion"], 3) if totals["completion"] else None,
    }


def load_pricing_profile(path: str) -> dict[str, Any]:
    """Load a validated, external pricing profile without exposing its path."""
    if not path:
        return {"status": "not_claimed", "reason": "no_pricing_profile_configured"}
    try:
        with open(path, "rb") as handle:
            raw = handle.read()
        payload = json.loads(raw.decode("utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        return {"status": "invalid", "reason": "pricing_profile_unreadable"}
    if not isinstance(payload, dict):
        return {"status": "invalid", "reason": "pricing_profile_schema"}
    profile = str(payload.get("profile") or "").strip()
    currency = str(payload.get("currency") or "").strip().upper()
    try:
        prompt = float(payload.get("prompt_usd_per_1k"))
        completion = float(payload.get("completion_usd_per_1k"))
    except (TypeError, ValueError):
        return {"status": "invalid", "reason": "pricing_profile_rates"}
    if not profile or len(profile) > 128 or not re.fullmatch(r"[A-Za-z0-9._:-]+", profile):
        return {"status": "invalid", "reason": "pricing_profile_name"}
    if currency != "USD" or not all(math.isfinite(value) and value >= 0 for value in (prompt, completion)):
        return {"status": "invalid", "reason": "pricing_profile_rates"}
    return {
        "status": "valid",
        "profile": profile,
        "currency": currency,
        "prompt_usd_per_1k": prompt,
        "completion_usd_per_1k": completion,
        "pricing_fingerprint": hashlib.sha256(raw).hexdigest(),
    }


def estimate_model_cost(tokens: dict[str, Any], pricing: dict[str, Any], generation_samples: int | None = None) -> dict[str, Any]:
    """Estimate USD cost only from validated prices and observed token counts."""
    if pricing.get("status") != "valid":
        return {"status": pricing.get("status", "not_claimed"), "reason": pricing.get("reason", "pricing_unavailable")}
    prompt_tokens = max(0, safe_int(str(tokens.get("prompt_tokens", 0))))
    completion_tokens = max(0, safe_int(str(tokens.get("completion_tokens", 0))))
    prompt_cost = prompt_tokens / 1000 * float(pricing["prompt_usd_per_1k"])
    completion_cost = completion_tokens / 1000 * float(pricing["completion_usd_per_1k"])
    total = prompt_cost + completion_cost
    samples = max(0, int(generation_samples or 0))
    return {
        "status": "estimated",
        "currency": pricing["currency"],
        "profile": pricing["profile"],
        "pricing_fingerprint": pricing["pricing_fingerprint"],
        "prompt_tokens": prompt_tokens,
        "completion_tokens": completion_tokens,
        "prompt_cost": round(prompt_cost, 8),
        "completion_cost": round(completion_cost, 8),
        "total_cost": round(total, 8),
        "prompt_share": round(prompt_cost / total * 100, 3) if total else None,
        "completion_share": round(completion_cost / total * 100, 3) if total else None,
        "generation_samples": samples,
        "avg_cost_per_generation": round(total / samples, 8) if samples else None,
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


def load_traffic_attestation(path: str | None) -> dict[str, Any]:
    """Load a privacy-safe traffic attestation, failing closed on bad input."""
    base = {"status": "not_claimed", "source": "no_attestation_configured"}
    if not path:
        return base
    try:
        with open(path, encoding="utf-8") as handle:
            payload = json.load(handle)
    except (OSError, json.JSONDecodeError):
        return {"status": "invalid", "source": "attestation_unreadable"}
    if not isinstance(payload, dict):
        return {"status": "invalid", "source": "attestation_not_object"}
    status = str(payload.get("status") or "").strip().lower()
    source = str(payload.get("source") or "").strip().lower()
    if status not in TRAFFIC_STATUSES or source not in TRAFFIC_SOURCES:
        return {"status": "invalid", "source": "attestation_enum_invalid"}
    if status == "production_attested" and not re.fullmatch(r"[0-9a-f]{64}", str(payload.get("attestation_fingerprint") or "").lower()):
        return {"status": "invalid", "source": "attestation_fingerprint_invalid"}
    try:
        request_count = int(payload.get("request_count"))
        tenant_count = int(payload.get("tenant_count"))
        user_count = int(payload.get("user_count"))
    except (TypeError, ValueError):
        return {"status": "invalid", "source": "attestation_count_invalid"}
    if min(request_count, tenant_count, user_count) < 0 or tenant_count > request_count or user_count > request_count:
        return {"status": "invalid", "source": "attestation_count_out_of_range"}
    window_start = str(payload.get("window_start") or "").strip()
    window_end = str(payload.get("window_end") or "").strip()
    try:
        start = datetime.fromisoformat(window_start.replace("Z", "+00:00"))
        end = datetime.fromisoformat(window_end.replace("Z", "+00:00"))
        if start.tzinfo is None or end.tzinfo is None or end <= start:
            raise ValueError
    except ValueError:
        return {"status": "invalid", "source": "attestation_window_invalid"}
    return {
        "status": status,
        "source": source,
        "window_start": window_start,
        "window_end": window_end,
        "request_count": request_count,
        "tenant_count": tenant_count,
        "user_count": user_count,
        "attested": status == "production_attested",
    }


def aggregate(traffic_attestation_path: str | None = None, pricing_profile_path: str | None = None) -> dict[str, Any]:
    # A non-null finished_at is the durable terminal marker. `abandoned` is a
    # recovery/observability terminal state, not a business execution failure;
    # report it separately so stale-process cleanup cannot inflate failure rate.
    traces = mysql_query("""
      SELECT COUNT(*), COALESCE(SUM(CASE WHEN status IN ('success','completed') THEN 1 ELSE 0 END),0),
             COALESCE(SUM(CASE WHEN status NOT IN ('success','completed','abandoned') THEN 1 ELSE 0 END),0),
             COALESCE(SUM(CASE WHEN status = 'abandoned' THEN 1 ELSE 0 END),0),
             COALESCE(AVG(CASE WHEN status IN ('success','completed') THEN latency_ms END),0),
             COALESCE(AVG(CASE WHEN status NOT IN ('success','completed','abandoned') THEN latency_ms END),0),
             COALESCE(AVG(CASE WHEN status = 'abandoned' THEN latency_ms END),0)
      FROM ws_agent_trace WHERE finished_at IS NOT NULL
    """)[0]
    latency_by_agent = mysql_query("""
      SELECT agent_type, COUNT(*),
             COALESCE(AVG(CASE WHEN status IN ('success','completed') THEN latency_ms END),0),
             COALESCE(AVG(CASE WHEN status NOT IN ('success','completed','abandoned') THEN latency_ms END),0),
             COALESCE(AVG(latency_ms),0)
      FROM ws_agent_trace WHERE finished_at IS NOT NULL
      GROUP BY agent_type ORDER BY agent_type
    """)
    # The denominator is every finished Agent trace, including a terminal
    # trace that has no persisted step (for example an early validation or
    # admission failure).  Joining only ws_agent_trace_step would both exclude
    # those zero-step traces and accidentally include unfinished traces.
    steps = mysql_query("""
      SELECT COUNT(*), COALESCE(SUM(CASE WHEN step_count > 0 THEN 1 ELSE 0 END),0),
             COALESCE(AVG(step_count),0), COALESCE(MIN(step_count),0), COALESCE(MAX(step_count),0)
      FROM (
        SELECT t.trace_id, COUNT(s.id) step_count
        FROM ws_agent_trace t
        LEFT JOIN ws_agent_trace_step s ON s.trace_id = t.trace_id AND s.tenant_id = t.tenant_id
        WHERE t.finished_at IS NOT NULL
        GROUP BY t.trace_id
      ) s
    """)[0]
    trace_latency_rows = mysql_query("""
      SELECT status, latency_ms
      FROM ws_agent_trace
      WHERE finished_at IS NOT NULL AND latency_ms >= 0
    """)
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
    tool_latency_rows = mysql_query("""
      SELECT tool_name, status, latency_ms
      FROM ws_tool_call_record
      WHERE latency_ms >= 0
    """)
    index_task_rows = mysql_query("""
      SELECT status, REPLACE(REPLACE(COALESCE(error_msg, ''), CHAR(10), ' '), CHAR(13), ' '), COUNT(*)
      FROM ws_index_task
      GROUP BY status, error_msg
    """)
    index_task_recent_rows = mysql_query("""
      SELECT status, REPLACE(REPLACE(COALESCE(error_msg, ''), CHAR(10), ' '), CHAR(13), ' '), COUNT(*)
      FROM ws_index_task
      WHERE created_at >= NOW() - INTERVAL 24 HOUR
      GROUP BY status, error_msg
    """)
    ops_task_rows = mysql_query("""
      SELECT status, COUNT(*),
             COALESCE(SUM(CASE WHEN finished_at IS NOT NULL THEN 1 ELSE 0 END),0),
             COALESCE(SUM(CASE WHEN finished_at IS NOT NULL
                 THEN TIMESTAMPDIFF(MICROSECOND, created_at, finished_at) / 1000 ELSE 0 END),0)
      FROM ws_ops_task
      GROUP BY status
    """)
    ops_task_recent_rows = mysql_query("""
      SELECT status, COUNT(*),
             COALESCE(SUM(CASE WHEN finished_at IS NOT NULL THEN 1 ELSE 0 END),0),
             COALESCE(SUM(CASE WHEN finished_at IS NOT NULL
                 THEN TIMESTAMPDIFF(MICROSECOND, created_at, finished_at) / 1000 ELSE 0 END),0)
      FROM ws_ops_task
      WHERE created_at >= NOW() - INTERVAL 24 HOUR
      GROUP BY status
    """)
    approval_rows = mysql_query("""
      SELECT status,
             CASE WHEN status <> 'pending'
                  THEN TIMESTAMPDIFF(MICROSECOND, created_at, updated_at) / 1000
                  ELSE NULL END
      FROM ws_approval
    """)
    approval_recent_rows = mysql_query("""
      SELECT status,
             CASE WHEN status <> 'pending'
                  THEN TIMESTAMPDIFF(MICROSECOND, created_at, updated_at) / 1000
                  ELSE NULL END
      FROM ws_approval
      WHERE created_at >= NOW() - INTERVAL 24 HOUR
    """)
    feedback_rows = mysql_query("""
      SELECT target_type, rating
      FROM ws_feedback
    """)
    feedback_recent_rows = mysql_query("""
      SELECT target_type, rating
      FROM ws_feedback
      WHERE created_at >= NOW() - INTERVAL 24 HOUR
    """)
    metrics_text = fetch_raw_metrics(os.environ.get("METRICS_URL", "http://127.0.0.1:8090/metrics"))
    result: dict[str, Any] = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "trace": {"completed_or_success": safe_int(traces[1]), "failed_or_terminal_error": safe_int(traces[2]),
                   "abandoned": safe_int(traces[3]),
                   "finished": safe_int(traces[0]), "avg_latency_ms_success": round(safe_float(traces[4]), 2),
                   "avg_latency_ms_failed": round(safe_float(traces[5]), 2),
                   "avg_latency_ms_abandoned": round(safe_float(traces[6]), 2),
                   "avg_latency_ms_business_terminal": round((safe_float(traces[4]) * safe_int(traces[1]) + safe_float(traces[5]) * safe_int(traces[2])) / max(1, safe_int(traces[1]) + safe_int(traces[2])), 2),
                   "avg_latency_ms_all": round((safe_float(traces[4]) * safe_int(traces[1]) + safe_float(traces[5]) * safe_int(traces[2]) + safe_float(traces[6]) * safe_int(traces[3])) / max(1, safe_int(traces[1]) + safe_int(traces[2]) + safe_int(traces[3])), 2)},
        "steps": {"finished_traces": safe_int(steps[0]), "traces_with_steps": safe_int(steps[1]), "avg_per_trace": round(safe_float(steps[2]), 3),
                   "min": safe_int(steps[3]), "max": safe_int(steps[4])},
        "trace_latency": aggregate_trace_latency(trace_latency_rows),
        "by_agent_type": [{"agent_type": row[0], "traces": safe_int(row[1]), "avg_steps": round(safe_float(row[2]), 3)} for row in by_agent],
        "latency_by_agent_type": [{"agent_type": row[0], "traces": safe_int(row[1]),
                                    "avg_latency_ms_success": round(safe_float(row[2]), 2),
                                    "avg_latency_ms_failed": round(safe_float(row[3]), 2),
                                    "avg_latency_ms_all": round(safe_float(row[4]), 2)} for row in latency_by_agent],
        "tool_calls": {"total": safe_int(tools[0]), "successful": safe_int(tools[1]),
                        "success_rate": round(safe_int(tools[1]) / safe_int(tools[0]) * 100, 2) if safe_int(tools[0]) else None,
                        **aggregate_tool_latency(tool_latency_rows)},
        "index_tasks": aggregate_index_tasks(index_task_rows),
        "index_tasks_recent_24h": aggregate_index_tasks(index_task_recent_rows),
        "ops_tasks": aggregate_ops_tasks(ops_task_rows),
        "ops_tasks_recent_24h": aggregate_ops_tasks(ops_task_recent_rows),
        "approvals": aggregate_approvals(approval_rows),
        "approvals_recent_24h": aggregate_approvals(approval_recent_rows),
        "feedback": aggregate_feedback(feedback_rows),
        "feedback_recent_24h": aggregate_feedback(feedback_recent_rows),
        "traffic_evidence": load_traffic_attestation(traffic_attestation_path or os.environ.get("TRAFFIC_ATTESTATION_FILE", "")),
        "rag_retrieval": parse_prometheus(metrics_text) if metrics_text else fetch_metrics(os.environ.get("METRICS_URL", "http://127.0.0.1:8090/metrics")),
        "rag_inventory": parse_rag_inventory(metrics_text),
        "model_generation": parse_model_prometheus(metrics_text),
        "model_breaker": parse_breaker_events(metrics_text),
        "model_admission": parse_model_admission(metrics_text),
        "model_tokens": parse_model_tokens(metrics_text),
    }
    pricing = load_pricing_profile(pricing_profile_path or os.environ.get("MODEL_PRICING_PROFILE_FILE", ""))
    result["model_cost"] = estimate_model_cost(result["model_tokens"], pricing, result["model_generation"].get("sample_count"))
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    parser.add_argument("--traffic-attestation", default="", help="Path to an anonymous, validated traffic attestation JSON artifact.")
    parser.add_argument("--pricing-profile", default="", help="Path to an external, versioned USD model pricing profile.")
    args = parser.parse_args()
    try:
        report = aggregate(args.traffic_attestation, args.pricing_profile)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    if args.format == "json":
        print(json.dumps(report, ensure_ascii=False, indent=2))
    else:
        print("# Agent Quality Aggregate Report\n")
        print(f"- Generated (UTC): {report['generated_at']}")
        print(f"- Finished traces: {report['trace']['finished']}")
        print(f"- Abandoned traces: {report['trace']['abandoned']}")
        print(f"- Average Agent latency (success): {report['trace']['avg_latency_ms_success']} ms")
        print(f"- Average Agent latency (all finished): {report['trace']['avg_latency_ms_all']} ms")
        print(f"- Average Agent latency (business terminal): {report['trace']['avg_latency_ms_business_terminal']} ms")
        for label, key in (("success", "success"), ("business terminal", "business_terminal"), ("all finished", "all_finished")):
            latency = report["trace_latency"][key]
            print(f"- Agent latency {label}: samples={latency['samples']}, p50={latency['p50_ms'] if latency['p50_ms'] is not None else 'N/A'} ms, p95={latency['p95_ms'] if latency['p95_ms'] is not None else 'N/A'} ms")
        print(f"- Average Agent steps/trace: {report['steps']['avg_per_trace']}")
        print(f"- Tool success rate: {report['tool_calls']['success_rate'] if report['tool_calls']['success_rate'] is not None else 'N/A'}%")
        index_tasks = report["index_tasks"]
        print(f"- Index tasks: total={index_tasks['total']}, failed_or_dead={index_tasks['failed_or_dead']}, retryable_or_running={index_tasks['retryable_or_running']}, failure_rate={index_tasks['failure_rate'] if index_tasks['failure_rate'] is not None else 'N/A'}%")
        print(f"- Index task failure categories: {index_tasks['failure_categories']}")
        recent_index_tasks = report["index_tasks_recent_24h"]
        print(f"- Index tasks (last 24h): total={recent_index_tasks['total']}, failed_or_dead={recent_index_tasks['failed_or_dead']}, retryable_or_running={recent_index_tasks['retryable_or_running']}, failure_rate={recent_index_tasks['failure_rate'] if recent_index_tasks['failure_rate'] is not None else 'N/A'}%")
        print(f"- Index task failure categories (last 24h): {recent_index_tasks['failure_categories']}")
        ops_tasks = report["ops_tasks"]
        print(f"- Ops tasks: total={ops_tasks['total']}, finished={ops_tasks['finished']}, in_flight={ops_tasks['in_flight']}, completion_rate={ops_tasks['completion_rate'] if ops_tasks['completion_rate'] is not None else 'N/A'}%, failure_rate={ops_tasks['failure_rate'] if ops_tasks['failure_rate'] is not None else 'N/A'}%, avg_e2e={ops_tasks['avg_e2e_latency_ms'] if ops_tasks['avg_e2e_latency_ms'] is not None else 'N/A'} ms")
        recent_ops_tasks = report["ops_tasks_recent_24h"]
        print(f"- Ops tasks (last 24h): total={recent_ops_tasks['total']}, finished={recent_ops_tasks['finished']}, in_flight={recent_ops_tasks['in_flight']}, completion_rate={recent_ops_tasks['completion_rate'] if recent_ops_tasks['completion_rate'] is not None else 'N/A'}%, failure_rate={recent_ops_tasks['failure_rate'] if recent_ops_tasks['failure_rate'] is not None else 'N/A'}%, avg_e2e={recent_ops_tasks['avg_e2e_latency_ms'] if recent_ops_tasks['avg_e2e_latency_ms'] is not None else 'N/A'} ms")
        approvals = report["approvals"]
        print(f"- Approvals: total={approvals['total']}, decided={approvals['decided']}, pending={approvals['pending']}, decision_rate={approvals['decision_rate'] if approvals['decision_rate'] is not None else 'N/A'}%, decision_p95={approvals['decision_latency_p95_ms'] if approvals['decision_latency_p95_ms'] is not None else 'N/A'} ms")
        recent_approvals = report["approvals_recent_24h"]
        print(f"- Approvals (last 24h): total={recent_approvals['total']}, decided={recent_approvals['decided']}, pending={recent_approvals['pending']}, decision_rate={recent_approvals['decision_rate'] if recent_approvals['decision_rate'] is not None else 'N/A'}%, decision_p95={recent_approvals['decision_latency_p95_ms'] if recent_approvals['decision_latency_p95_ms'] is not None else 'N/A'} ms")
        feedback = report["feedback"]
        print(f"- User feedback: total={feedback['total']}, rated={feedback['rated']}, useful={feedback['useful']}, bad={feedback['bad']}, useful_rate={feedback['useful_rate'] if feedback['useful_rate'] is not None else 'N/A'}%")
        recent_feedback = report["feedback_recent_24h"]
        print(f"- User feedback (last 24h): total={recent_feedback['total']}, rated={recent_feedback['rated']}, useful={recent_feedback['useful']}, bad={recent_feedback['bad']}, useful_rate={recent_feedback['useful_rate'] if recent_feedback['useful_rate'] is not None else 'N/A'}%")
        print(f"- Production traffic evidence: {report['traffic_evidence']['status']} ({report['traffic_evidence']['source']})")
        print(f"- Tool dependency availability: attempts={report['tool_calls']['dependency_attempts']}, available={report['tool_calls']['dependency_available']}, availability_rate={report['tool_calls']['dependency_availability_rate'] if report['tool_calls']['dependency_availability_rate'] is not None else 'N/A'}%, response_rate={report['tool_calls']['dependency_response_rate'] if report['tool_calls']['dependency_response_rate'] is not None else 'N/A'}%")
        for tool in report["tool_calls"]["by_tool"]:
            zero_note = f", zero_latency={tool['zero_latency_samples']} ({tool['zero_latency_rate']}%)" if tool["zero_latency_samples"] else ""
            print(f"- Tool {tool['tool_name']}: calls={tool['calls']}, success_rate={tool['success_rate']}%, dependency_availability={tool['dependency_availability_rate'] if tool['dependency_availability_rate'] is not None else 'N/A'}%, response_rate={tool['dependency_response_rate'] if tool['dependency_response_rate'] is not None else 'N/A'}%, unavailable_rate={tool['unavailable_rate'] if tool['unavailable_rate'] is not None else 'N/A'}%, timeout_rate={tool['timeout_rate'] if tool['timeout_rate'] is not None else 'N/A'}%, p50={tool['p50_ms']} ms, p95={tool['p95_ms']} ms, success_p95={tool['success_p95_ms']} ms{zero_note}")
        rag = report["rag_retrieval"]
        inventory = report["rag_inventory"]
        print(f"- RAG inventory: active_documents={inventory['active_documents'] if inventory['active_documents'] is not None else 'N/A'}, published_chunks={inventory['active_published_chunks'] if inventory['active_published_chunks'] is not None else 'N/A'}, legacy_documents={inventory['active_legacy_documents'] if inventory['active_legacy_documents'] is not None else 'N/A'}, physical_vectors={inventory['physical_vectors'] if inventory['physical_vectors'] is not None else 'N/A'}")
        print(f"- RAG retrieval samples: {rag['sample_count']}")
        print(f"- RAG successful samples: {rag.get('success_sample_count', 0)}")
        print(f"- RAG error samples: {rag.get('error_sample_count', 0)}")
        print(f"- RAG successful retrieval P95: {rag.get('success_p95_ms') if rag.get('success_p95_ms') is not None else 'N/A'} ms")
        print(f"- RAG successful retrieval mean: {rag.get('success_mean_ms') if rag.get('success_mean_ms') is not None else 'N/A'} ms")
        if rag.get("p95_semantics"):
            print("- RAG P95 semantics: histogram bucket upper bound (not an exact percentile)")
        model = report["model_generation"]
        breaker = report["model_breaker"]
        admission = report["model_admission"]
        print(f"- Model admission: rejections={admission['rejections']}, in_flight={admission['in_flight']}, rejection_rate={admission['rejection_rate'] if admission['rejection_rate'] is not None else 'N/A'}%")
        print(f"- Model admission decision wait: accepted={admission['mean_wait_accepted_ms'] if admission['mean_wait_accepted_ms'] is not None else 'N/A'} ms, rejected={admission['mean_wait_rejected_ms'] if admission['mean_wait_rejected_ms'] is not None else 'N/A'} ms")
        print(f"- Model breaker events: open={breaker['open']}, rejected={breaker['rejected']}, probe={breaker['probe']}, closed={breaker['closed']}")
        print(f"- Model generation samples: {model['sample_count']}")
        print(f"- Model generation success samples: {model['success_sample_count']}")
        print(f"- Model generation timeout samples: {model['timeout_sample_count']}")
        print(f"- Model generation successful P95: {model['success_p95_ms'] if model['success_p95_ms'] is not None else 'N/A'} ms")
        print(f"- Model generation successful mean: {model['success_mean_ms'] if model['success_mean_ms'] is not None else 'N/A'} ms")
        cost = report["model_cost"]
        if cost.get("status") == "estimated":
            print(f"- Model cost estimate: {cost['total_cost']} {cost['currency']} (prompt={cost['prompt_cost']}, completion={cost['completion_cost']}, avg/generation={cost['avg_cost_per_generation'] if cost['avg_cost_per_generation'] is not None else 'N/A'}, profile={cost['profile']})")
        else:
            print(f"- Model cost estimate: N/A ({cost.get('reason', cost.get('status', 'unavailable'))})")
        if rag["sample_count"] < 30 or rag.get("success_sample_count", 0) < 30:
            print("\n> RAG P95 is provisional when fewer than 30 total and successful retrieval samples exist; this is not a production SLA.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
