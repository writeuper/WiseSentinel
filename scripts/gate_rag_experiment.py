#!/usr/bin/env python3
"""Apply conservative release gates to a comparable RAG experiment pair.

Inputs are aggregate evaluation summaries from run_agent_eval.py. The gate does
not print queries, document IDs, tenant IDs, traces, or model credentials.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from compare_agent_eval import compare_summaries, load_summary


def metric(summary: dict[str, Any], path: str) -> float | None:
    current: Any = summary
    for part in path.split("."):
        if not isinstance(current, dict):
            return None
        current = current.get(part)
    return float(current) if isinstance(current, (int, float)) and not isinstance(current, bool) else None


def evaluate(before: dict[str, Any], after: dict[str, Any], min_verified: int = 100, max_p95_regression: float = 0.10) -> dict[str, Any]:
    comparison = compare_summaries(before, after)
    report: dict[str, Any] = {"comparison_status": comparison["status"], "checks": [], "passed": False}
    if comparison["status"] != "ok":
        report["checks"].append({"name": "provenance", "passed": False, "reason": "evaluation metadata differs"})
        return report

    verified_before = metric(before, "rag_ranking_verified.sample_count")
    verified_after = metric(after, "rag_ranking_verified.sample_count")
    verified_ok = verified_before is not None and verified_after is not None and verified_before >= min_verified and verified_after >= min_verified
    report["checks"].append({"name": "verified_labels", "passed": verified_ok, "minimum": min_verified, "before": verified_before, "after": verified_after})

    for name in ("recall_at_3", "mrr", "ndcg_at_5"):
        before_value = metric(before, "rag_ranking_verified." + name)
        after_value = metric(after, "rag_ranking_verified." + name)
        passed = before_value is not None and after_value is not None and after_value >= before_value
        report["checks"].append({"name": "verified_" + name, "passed": passed, "before": before_value, "after": after_value})

    before_p95 = metric(before, "latency_ms.p95")
    after_p95 = metric(after, "latency_ms.p95")
    latency_ok = before_p95 is not None and after_p95 is not None and after_p95 <= before_p95 * (1 + max_p95_regression)
    report["checks"].append({"name": "p95_latency", "passed": latency_ok, "before_ms": before_p95, "after_ms": after_p95, "max_regression_ratio": max_p95_regression})
    report["passed"] = all(check["passed"] for check in report["checks"])
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before", required=True, type=Path)
    parser.add_argument("--after", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--min-verified", type=int, default=100)
    parser.add_argument("--max-p95-regression", type=float, default=0.10)
    args = parser.parse_args()
    try:
        if args.min_verified < 1 or args.max_p95_regression < 0:
            raise ValueError("gate thresholds must be non-negative and min-verified positive")
        report = evaluate(load_summary(args.before), load_summary(args.after), args.min_verified, args.max_p95_regression)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"RAG experiment gate failed: {exc}", file=sys.stderr)
        return 2
    return 0 if report["passed"] else 3


if __name__ == "__main__":
    raise SystemExit(main())
