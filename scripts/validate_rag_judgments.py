#!/usr/bin/env python3
"""Validate double-reviewed RAG relevance judgments and emit a gold-label set.

Input CSV rows use the privacy-safe schema:
``case_id,annotator_id,doc_id,relevance,review_status``.
The script emits aggregate quality information and, when requested, only
adjudicated cases as ``verified`` relevance labels. It never prints queries,
document content, annotator names or document IDs.
"""

from __future__ import annotations

import argparse
import csv
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

RELEVANCE_VALUES = {"relevant", "not_relevant"}
STATUS_VALUES = {"draft", "adjudicated"}


def read_rows(path: Path) -> list[dict[str, str]]:
    with path.open(encoding="utf-8-sig", newline="") as handle:
        rows = list(csv.DictReader(handle))
    required = {"case_id", "annotator_id", "doc_id", "relevance", "review_status"}
    missing = required - set(rows[0].keys() if rows else [])
    if missing:
        raise ValueError(f"judgment CSV missing columns: {', '.join(sorted(missing))}")
    return [{key: str(value or "").strip() for key, value in row.items()} for row in rows]


def validate_rows(rows: list[dict[str, str]]) -> list[str]:
    errors: list[str] = []
    seen: set[tuple[str, str, str]] = set()
    for index, row in enumerate(rows, start=2):
        case_id, annotator, doc = row.get("case_id", ""), row.get("annotator_id", ""), row.get("doc_id", "")
        key = (case_id, annotator, doc)
        if not case_id or not annotator or not doc:
            errors.append(f"row {index}: case_id/annotator_id/doc_id must be non-empty")
        if key in seen:
            errors.append(f"row {index}: duplicate judgment key")
        seen.add(key)
        if row.get("relevance", "").lower() not in RELEVANCE_VALUES:
            errors.append(f"row {index}: relevance must be relevant or not_relevant")
        if row.get("review_status", "").lower() not in STATUS_VALUES:
            errors.append(f"row {index}: review_status must be draft or adjudicated")
    return errors


def _relevant_docs(rows: list[dict[str, str]], annotator: str) -> set[str]:
    return {row["doc_id"] for row in rows if row["annotator_id"] == annotator and row["relevance"].lower() == "relevant"}


def _jaccard(left: set[str], right: set[str]) -> float:
    union = left | right
    return 1.0 if not union else len(left & right) / len(union)


def build_report(rows: list[dict[str, str]]) -> dict[str, Any]:
    grouped: dict[str, list[dict[str, str]]] = defaultdict(list)
    for row in rows:
        grouped[row["case_id"]].append(row)
    case_stats: list[dict[str, Any]] = []
    for case_id, case_rows in grouped.items():
        annotators = sorted({row["annotator_id"] for row in case_rows})
        sets = [_relevant_docs(case_rows, annotator) for annotator in annotators]
        pairwise = [_jaccard(sets[i], sets[j]) for i in range(len(sets)) for j in range(i + 1, len(sets))]
        exact = len(sets) >= 2 and all(candidate == sets[0] for candidate in sets[1:])
        adjudicated = bool(case_rows) and all(row["review_status"].lower() == "adjudicated" for row in case_rows)
        case_stats.append({
            "annotators": len(annotators),
            "adjudicated": adjudicated,
            "double_reviewed": len(annotators) >= 2,
            "exact_agreement": exact,
            "jaccard": sum(pairwise) / len(pairwise) if pairwise else None,
        })
    eligible = [item for item in case_stats if item["double_reviewed"] and item["adjudicated"] and item["exact_agreement"]]
    compared = [item for item in case_stats if item["double_reviewed"]]
    agreement = [item for item in compared if item["exact_agreement"]]
    jaccards = [item["jaccard"] for item in compared if item["jaccard"] is not None]
    return {
        "case_count": len(case_stats),
        "annotator_count": len({row["annotator_id"] for row in rows}),
        "row_count": len(rows),
        "eligible_verified_cases": len(eligible),
        "double_reviewed_cases": len(compared),
        "agreement_cases": len(agreement),
        "exact_agreement_rate": len(agreement) / len(compared) if compared else None,
        "mean_jaccard": sum(jaccards) / len(jaccards) if jaccards else None,
        "review_status_counts": dict(sorted(Counter(row["review_status"].lower() for row in rows).items())),
        "validation_errors": [],
    }


def build_gold_rows(rows: list[dict[str, str]]) -> list[dict[str, str]]:
    grouped: dict[str, list[dict[str, str]]] = defaultdict(list)
    for row in rows:
        grouped[row["case_id"]].append(row)
    result = []
    for case_id, case_rows in sorted(grouped.items()):
        annotators = sorted({row["annotator_id"] for row in case_rows})
        if len(annotators) < 2 or not all(row["review_status"].lower() == "adjudicated" for row in case_rows):
            continue
        sets = [_relevant_docs(case_rows, annotator) for annotator in annotators]
        if not all(candidate == sets[0] for candidate in sets[1:]):
            continue
        result.append({
            "case_id": case_id,
            "relevant_doc_ids": "|".join(sorted(sets[0])),
            "relevance_label_source": "expert_review",
            "relevance_label_status": "verified",
        })
    return result


def run(rows: list[dict[str, str]], minimum_verified: int = 100) -> tuple[dict[str, Any], list[dict[str, str]]]:
    errors = validate_rows(rows)
    report = build_report(rows)
    report["validation_errors"] = errors
    gold = [] if errors else build_gold_rows(rows)
    if len(gold) < minimum_verified:
        report["minimum_verified_required"] = minimum_verified
        report["strict_gate_passed"] = False
    else:
        report["minimum_verified_required"] = minimum_verified
        report["strict_gate_passed"] = True
    return report, gold


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--summary-json", type=Path, required=True)
    parser.add_argument("--gold-output", type=Path)
    parser.add_argument("--min-verified-cases", type=int, default=100)
    parser.add_argument("--allow-incomplete", action="store_true", help="Write diagnostics even when the strict sample gate fails.")
    args = parser.parse_args()
    try:
        report, gold = run(read_rows(args.input), max(1, args.min_verified_cases))
    except (OSError, ValueError, csv.Error) as exc:
        print(f"RAG judgment validation failed: {exc}", file=sys.stderr)
        return 2
    args.summary_json.parent.mkdir(parents=True, exist_ok=True)
    args.summary_json.write_text(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if args.gold_output:
        args.gold_output.parent.mkdir(parents=True, exist_ok=True)
        with args.gold_output.open("w", encoding="utf-8-sig", newline="") as handle:
            writer = csv.DictWriter(handle, fieldnames=["case_id", "relevant_doc_ids", "relevance_label_source", "relevance_label_status"])
            writer.writeheader()
            writer.writerows(gold)
    if report["validation_errors"]:
        print(f"validation errors: {len(report['validation_errors'])}", file=sys.stderr)
        return 2
    if not report["strict_gate_passed"] and not args.allow_incomplete:
        print(f"strict RAG label gate failed: verified={len(gold)} required={report['minimum_verified_required']}", file=sys.stderr)
        return 3
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
