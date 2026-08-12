#!/usr/bin/env python3
"""Run an isolated, cleanup-safe concurrent Chat smoke/load scenario.

The script deliberately stores aggregate timings and error categories only.
It never writes API keys, questions, model answers, trace payloads or raw HTTP
bodies to its summary artifact.
"""

import argparse
import concurrent.futures
import json
import math
import sys
import time
from collections import Counter
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


DEFAULT_QUESTION = "请简要说明企业级 Agent 平台健康检查应覆盖哪些基础组件。"


def percentile(values: list[float], fraction: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    return ordered[max(0, math.ceil(len(ordered) * fraction) - 1)]


def parse_model_metric_totals(text: str) -> dict[str, float]:
    """Return aggregate model counters without retaining Prometheus labels."""
    totals = {
        "calls": 0.0,
        "duration_seconds": 0.0,
        "admission_rejections": 0.0,
        "admission_wait_accepted_seconds": 0.0,
        "admission_wait_accepted_count": 0.0,
        "admission_wait_rejected_seconds": 0.0,
        "admission_wait_rejected_count": 0.0,
    }
    for line in text.splitlines():
        try:
            metric, value = line.rsplit(" ", 1)
            parsed = float(value)
        except ValueError:
            continue
        if metric.startswith("ws_model_calls_total{"):
            totals["calls"] += parsed
        elif metric.startswith("ws_model_call_duration_seconds_sum{"):
            totals["duration_seconds"] += parsed
        elif metric.startswith("ws_model_admission_rejections_total{"):
            totals["admission_rejections"] += parsed
        elif metric.startswith("ws_model_admission_wait_seconds_sum{"):
            if 'outcome="accepted"' in metric:
                totals["admission_wait_accepted_seconds"] += parsed
            elif 'outcome="rejected"' in metric:
                totals["admission_wait_rejected_seconds"] += parsed
        elif metric.startswith("ws_model_admission_wait_seconds_count{"):
            if 'outcome="accepted"' in metric:
                totals["admission_wait_accepted_count"] += parsed
            elif 'outcome="rejected"' in metric:
                totals["admission_wait_rejected_count"] += parsed
    return totals


class Client:
    def __init__(self, base_url: str, api_key: str, tenant_id: str, timeout: int, bearer_token: str = "") -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.tenant_id = tenant_id
        self.timeout = timeout
        self.bearer_token = bearer_token.strip()

    def auth_headers(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self.bearer_token}"} if self.bearer_token else {"X-API-Key": self.api_key}

    def request(self, method: str, path: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8") if payload is not None else None
        request = Request(
            f"{self.base_url}{path}", data=body,
            headers={"Accept": "application/json", "Content-Type": "application/json", "X-Tenant-ID": self.tenant_id, **self.auth_headers()},
            method=method,
        )
        try:
            with urlopen(request, timeout=self.timeout) as response:
                raw = response.read().decode("utf-8")
        except HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="ignore")
            try:
                envelope = json.loads(raw)
            except json.JSONDecodeError:
                envelope = None
            if isinstance(envelope, dict) and envelope.get("code"):
                # Preserve only the stable application code in the aggregate;
                # never retain an upstream/model error body in load artifacts.
                raise RuntimeError(f"api_{envelope['code']}") from exc
            if exc.code == 429:
                raise RuntimeError("rate_limited") from exc
            raise RuntimeError(f"http_{exc.code}") from exc
        except URLError as exc:
            raise RuntimeError("network_error") from exc
        except TimeoutError as exc:
            raise RuntimeError("client_timeout") from exc
        try:
            envelope = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError("invalid_json") from exc
        if not isinstance(envelope, dict) or envelope.get("code") != 0:
            code = envelope.get("code") if isinstance(envelope, dict) else "invalid"
            raise RuntimeError(f"api_{code}")
        data = envelope.get("data")
        return data if isinstance(data, dict) else {}

    def current_user(self) -> dict[str, Any]:
        return self.request("GET", "/me")

    def model_metric_totals(self) -> dict[str, float] | None:
        parts = urlsplit(self.base_url)
        url = f"{parts.scheme}://{parts.netloc}/metrics"
        try:
            with urlopen(Request(url, headers={"Accept": "text/plain"}), timeout=5) as response:
                return parse_model_metric_totals(response.read().decode("utf-8"))
        except (HTTPError, URLError, TimeoutError, UnicodeDecodeError):
            return None

    def run_one(self, ordinal: int, question: str) -> dict[str, Any]:
        session_id = ""
        started = time.monotonic()
        result: dict[str, Any]
        try:
            created = self.request("POST", "/sessions", {"title": f"load-eval-{ordinal}", "agent_type": "chat"})
            session_id = str(created.get("session_id") or "")
            if not session_id:
                raise RuntimeError("missing_session_id")
            chat = self.request("POST", "/chat", {"session_id": session_id, "question": question, "options": {"enable_rag": False, "enable_tools": False}})
            if not chat.get("trace_id"):
                raise RuntimeError("missing_trace_id")
            result = {"outcome": "success", "latency_ms": round((time.monotonic() - started) * 1000)}
        except RuntimeError as exc:
            result = {"outcome": str(exc), "latency_ms": round((time.monotonic() - started) * 1000)}
        finally:
            if session_id:
                try:
                    # A local 50304 means model admission failed before the
                    # Agent can produce a durable answer. Verify this retryable
                    # overload did not create a ghost conversation turn.
                    if result.get("outcome") == "api_50304":
                        try:
                            history = self.request("GET", f"/sessions/{session_id}/messages")
                            result["overload_history"] = "empty" if not (history.get("messages") or []) else "unexpected_messages"
                        except RuntimeError:
                            result["overload_history"] = "unreadable"
                    self.request("DELETE", f"/sessions/{session_id}")
                    try:
                        self.request("GET", f"/sessions/{session_id}/messages")
                    except RuntimeError as exc:
                        # A deleted session must cross the same durable
                        # authorization fence as chat/history reads. Do not
                        # infer cleanup from a queue count shared with older
                        # test data.
                        result["cleanup"] = "verified" if str(exc) in {"http_404", "api_40401"} else "failed"
                    else:
                        result["cleanup"] = "failed"
                except RuntimeError:
                    # Keep the request outcome intact; cleanup is reported as
                    # a separate aggregate so it cannot conceal user-path
                    # failures or leave a false success signal.
                    result["cleanup"] = "failed"
            else:
                result["cleanup"] = "not_created"
        return result


def build_summary(results: list[dict[str, Any]]) -> dict[str, Any]:
    latencies = [float(row["latency_ms"]) for row in results]
    successes = [row for row in results if row["outcome"] == "success"]
    latency_by_outcome: dict[str, dict[str, Any]] = {}
    for outcome in sorted({str(row["outcome"]) for row in results}):
        values = [float(row["latency_ms"]) for row in results if str(row["outcome"]) == outcome]
        latency_by_outcome[outcome] = {
            "count": len(values),
            "p50": percentile(values, 0.50),
            "p95": percentile(values, 0.95),
            "p99": percentile(values, 0.99),
            "max": max(values) if values else None,
        }
    return {
        "requests": len(results),
        "successes": len(successes),
        "success_rate": len(successes) / len(results) if results else None,
        "outcomes": dict(sorted(Counter(str(row["outcome"]) for row in results).items())),
        "cleanup": dict(sorted(Counter(str(row.get("cleanup") or "not_recorded") for row in results).items())),
        "overload_history": dict(sorted(Counter(str(row["overload_history"]) for row in results if row.get("overload_history")).items())),
        "latency_ms": {"p50": percentile(latencies, 0.50), "p95": percentile(latencies, 0.95), "p99": percentile(latencies, 0.99), "max": max(latencies) if latencies else None},
        "latency_by_outcome": latency_by_outcome,
    }


def metric_delta(before: dict[str, float] | None, after: dict[str, float] | None) -> dict[str, float] | None:
    if before is None or after is None:
        return None
    return {key: max(0.0, after.get(key, 0.0) - before.get(key, 0.0)) for key in before}


def quality_gate(summary: dict[str, Any], min_success_rate: float, max_p95_ms: int) -> list[str]:
    failures: list[str] = []
    if (summary.get("success_rate") or 0.0) < min_success_rate:
        failures.append("success_rate")
    if summary.get("cleanup") != {"verified": summary.get("requests", 0)}:
        failures.append("cleanup")
    overload_history = summary.get("overload_history") or {}
    if overload_history and overload_history != {"empty": sum(overload_history.values())}:
        failures.append("overload_history")
    outcomes = summary.get("outcomes") or {}
    metrics = summary.get("model_metrics_delta")
    api_overload_count = int(outcomes.get("api_50304", 0) or 0)
    if metrics is None:
        failures.append("admission_attribution_unavailable")
    else:
        admission_rejections = int(round(float(metrics.get("admission_rejections", 0.0) or 0.0)))
        if api_overload_count != admission_rejections:
            failures.append("admission_attribution")
    p95 = (summary.get("latency_ms") or {}).get("p95")
    if max_p95_ms > 0 and (p95 is None or p95 > max_p95_ms):
        failures.append("p95_latency")
    return failures


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run isolated concurrent WiseSentinel Chat requests.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090/api/v1")
    parser.add_argument("--api-key", required=True)
    parser.add_argument("--bearer-token", default="", help="Pre-provisioned test JWT; takes precedence over --api-key.")
    parser.add_argument("--tenant-id", default="default")
    parser.add_argument("--requests", type=int, default=4)
    parser.add_argument("--concurrency", type=int, default=2)
    parser.add_argument("--timeout", type=int, default=120)
    parser.add_argument("--min-success-rate", type=float, default=1.0)
    parser.add_argument("--max-p95-ms", type=int, default=0, help="0 disables the latency gate; set per environment SLO.")
    parser.add_argument("--question", default=DEFAULT_QUESTION)
    parser.add_argument("--summary-json", default="")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.requests <= 0 or args.concurrency <= 0 or args.concurrency > args.requests or not 0 <= args.min_success_rate <= 1 or args.max_p95_ms < 0:
        print("requests and concurrency must be positive, with concurrency <= requests", file=sys.stderr)
        return 2
    client = Client(args.base_url, args.api_key, args.tenant_id, args.timeout, args.bearer_token)
    try:
        authenticated_tenant = str(client.current_user().get("tenant_id") or "").strip()
    except RuntimeError as exc:
        print(f"test identity validation failed: {exc}", file=sys.stderr)
        return 2
    if not authenticated_tenant:
        print("authenticated test identity does not contain tenant_id", file=sys.stderr)
        return 2
    if args.tenant_id != authenticated_tenant:
        print("requested tenant does not match authenticated test identity", file=sys.stderr)
        return 2
    before_metrics = client.model_metric_totals()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        results = list(executor.map(lambda index: client.run_one(index, args.question), range(1, args.requests + 1)))
    summary = build_summary(results)
    summary["model_metrics_delta"] = metric_delta(before_metrics, client.model_metric_totals())
    failures = quality_gate(summary, args.min_success_rate, args.max_p95_ms)
    summary["quality_gate"] = {"passed": not failures, "failures": failures}
    print(json.dumps(summary, ensure_ascii=False, sort_keys=True))
    if args.summary_json:
        target = Path(args.summary_json)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(summary, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return 0 if not failures else 1


if __name__ == "__main__":
    raise SystemExit(main())
