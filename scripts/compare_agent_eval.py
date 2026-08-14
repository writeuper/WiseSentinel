#!/usr/bin/env python3
"""Compare two privacy-safe Agent evaluation summaries.

The comparison is intentionally conservative: metrics are only delta-compared
when the dataset, model profile and embedding profile match. Confidence
interval overlap is reported as a descriptive signal, not as a significance
test or a causal claim.
"""

from __future__ import annotations

import argparse
import json
import math
import sys
from pathlib import Path
from typing import Any


METRICS = {
    "overall_pass_rate": "higher",
    "business_pass_rate": "higher",
    "route_accuracy": "higher",
    "business_route_accuracy": "higher",
    "tool_success_rate": "higher",
    "business_tool_success_rate": "higher",
    "keyword_hit_rate": "higher",
    "business_keyword_hit_rate": "higher",
    "citation_validity_rate": "higher",
    "citation_version_rate": "higher",
    "citation_grounding_rate": "higher",
    "latency_ms.p50": "lower",
    "latency_ms.p95": "lower",
    "rag_ranking.recall_at_1": "higher",
    "rag_ranking.recall_at_3": "higher",
    "rag_ranking.recall_at_5": "higher",
    "rag_ranking.mrr": "higher",
    "rag_ranking.ndcg_at_5": "higher",
    "rag_ranking_verified.recall_at_1": "higher",
    "rag_ranking_verified.recall_at_3": "higher",
    "rag_ranking_verified.recall_at_5": "higher",
    "rag_ranking_verified.mrr": "higher",
    "rag_ranking_verified.ndcg_at_5": "higher",
}


def load_summary(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f"summary must be a JSON object: {path}")
    return value


def nested_get(value: dict[str, Any], path: str) -> Any:
    current: Any = value
    for part in path.split("."):
        if not isinstance(current, dict):
            return None
        current = current.get(part)
    return current


def comparable_metadata(before: dict[str, Any], after: dict[str, Any]) -> dict[str, Any]:
    before_meta = before.get("evaluation_metadata") if isinstance(before.get("evaluation_metadata"), dict) else {}
    after_meta = after.get("evaluation_metadata") if isinstance(after.get("evaluation_metadata"), dict) else {}
    keys = ("dataset_sha256", "model_profile", "embedding_profile", "environment")
    mismatches = {
        key: {"before": before_meta.get(key), "after": after_meta.get(key)}
        for key in keys
        if before_meta.get(key) != after_meta.get(key)
    }
    return {"comparable": not mismatches, "mismatches": mismatches}


def interval_overlap(left: Any, right: Any) -> bool | None:
    if not isinstance(left, (list, tuple)) or not isinstance(right, (list, tuple)) or len(left) != 2 or len(right) != 2:
        return None
    try:
        return max(float(left[0]), float(right[0])) <= min(float(left[1]), float(right[1]))
    except (TypeError, ValueError):
        return None


def compare_summaries(before: dict[str, Any], after: dict[str, Any], allow_incompatible: bool = False) -> dict[str, Any]:
    compatibility = comparable_metadata(before, after)
    result: dict[str, Any] = {"comparable": compatibility["comparable"], "metadata": compatibility}
    if not compatibility["comparable"] and not allow_incompatible:
        result["status"] = "incomparable"
        result["deltas"] = {}
        return result

    deltas: dict[str, Any] = {}
    for path, direction in METRICS.items():
        old, new = nested_get(before, path), nested_get(after, path)
        if not isinstance(old, (int, float)) or isinstance(old, bool) or not isinstance(new, (int, float)) or isinstance(new, bool):
            continue
        if not (math.isfinite(float(old)) and math.isfinite(float(new))):
            continue
        delta = float(new) - float(old)
        relative = None if float(old) == 0 else delta / abs(float(old))
        deltas[path] = {
            "before": old,
            "after": new,
            "delta": round(delta, 6),
            "relative_delta": round(relative, 6) if relative is not None else None,
            "direction": direction,
            "improved": delta > 0 if direction == "higher" else delta < 0,
        }

    for metric, ci_key in (("overall_pass_rate", "overall_pass_ci95"), ("business_pass_rate", "business_pass_ci95")):
        if metric in deltas:
            deltas[metric]["ci95_overlap"] = interval_overlap(before.get(ci_key), after.get(ci_key))
    result["status"] = "ok" if compatibility["comparable"] else "incompatible_allowed"
    result["deltas"] = deltas
    result["sample_sizes"] = {
        "before_executed": before.get("executed_cases"),
        "after_executed": after.get("executed_cases"),
        "before_business": before.get("business_cases"),
        "after_business": after.get("business_cases"),
        "before_rag_verified": nested_get(before, "rag_ranking_verified.sample_count"),
        "after_rag_verified": nested_get(after, "rag_ranking_verified.sample_count"),
    }
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before", required=True, type=Path)
    parser.add_argument("--after", required=True, type=Path)
    parser.add_argument("--output", type=Path, default=None)
    parser.add_argument("--allow-incompatible", action="store_true", help="Emit deltas even when provenance differs; mark them incompatible.")
    args = parser.parse_args()
    try:
        result = compare_summaries(load_summary(args.before), load_summary(args.after), args.allow_incompatible)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"comparison failed: {exc}", file=sys.stderr)
        return 2
    encoded = json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded, encoding="utf-8")
    else:
        print(encoded, end="")
    return 0 if result["status"] == "ok" else 3


if __name__ == "__main__":
    raise SystemExit(main())
