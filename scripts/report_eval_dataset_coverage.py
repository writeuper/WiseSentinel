#!/usr/bin/env python3
"""Report structural coverage and trust signals for an Agent eval CSV."""

from __future__ import annotations

import argparse
import csv
import json
import sys
from collections import Counter
from pathlib import Path
from typing import Any


def split_pipe(value: str) -> list[str]:
    return [item.strip() for item in (value or "").split("|") if item.strip()]


def read_cases(path: Path) -> list[dict[str, str]]:
    with path.open(encoding="utf-8-sig", newline="") as handle:
        return [dict(row) for row in csv.DictReader(handle)]


def analyze_cases(rows: list[dict[str, str]], minimum_cases: int = 100) -> dict[str, Any]:
    case_ids = [str(row.get("case_id") or "").strip() for row in rows]
    duplicates = sorted(case_id for case_id, count in Counter(case_ids).items() if case_id and count > 1)
    routes = Counter(str(row.get("expected_route") or "").strip() or "unknown" for row in rows)
    scenes = Counter(str(row.get("scene") or "").strip() or "unknown" for row in rows)
    tools = Counter()
    expected_tool_cases = forbidden_tool_cases = assertion_cases = source_cases = 0
    relevance_labeled = relevance_verified = 0
    missing_input = missing_route = 0
    for row in rows:
        expected = split_pipe(row.get("expected_tools", ""))
        forbidden = split_pipe(row.get("forbidden_tools", ""))
        if expected:
            expected_tool_cases += 1
            tools.update(expected)
        if forbidden:
            forbidden_tool_cases += 1
            tools.update(forbidden)
        has_assertion = bool(expected or forbidden or split_pipe(row.get("expected_source", "")) or
                             split_pipe(row.get("expected_knowledge", "")) or split_pipe(row.get("expected_keywords", "")))
        assertion_cases += int(has_assertion)
        source_cases += int(bool(split_pipe(row.get("expected_source", ""))))
        if split_pipe(row.get("relevant_doc_ids", "")):
            relevance_labeled += 1
            if ((row.get("relevance_label_status") or "").strip().lower() == "verified" and
                    (row.get("relevance_label_source") or "").strip().lower() in {"human", "expert_review", "golden_set"}):
                relevance_verified += 1
        missing_input += int(not str(row.get("input") or "").strip())
        missing_route += int(not str(row.get("expected_route") or "").strip())
    total = len(rows)
    return {
        "total_cases": total,
        "minimum_cases": minimum_cases,
        "case_count_gate": "passed" if total >= minimum_cases else "failed",
        "duplicate_case_ids": duplicates,
        "duplicate_case_id_count": len(duplicates),
        "routes": dict(sorted(routes.items())),
        "scenes": dict(sorted(scenes.items())),
        "tool_coverage": dict(sorted(tools.items())),
        "expected_tool_cases": expected_tool_cases,
        "forbidden_tool_cases": forbidden_tool_cases,
        "source_assertion_cases": source_cases,
        "assertion_cases": assertion_cases,
        "assertion_case_rate": round(assertion_cases / total * 100, 2) if total else None,
        "relevance_labeled_cases": relevance_labeled,
        "relevance_verified_cases": relevance_verified,
        "relevance_label_rate": round(relevance_labeled / total * 100, 2) if total else None,
        "relevance_verified_rate": round(relevance_verified / total * 100, 2) if total else None,
        "missing_input_cases": missing_input,
        "missing_route_cases": missing_route,
        "structural_gate": "passed" if not duplicates and not missing_input and not missing_route else "failed",
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--minimum-cases", type=int, default=100)
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    args = parser.parse_args()
    try:
        report = analyze_cases(read_cases(args.input), max(1, args.minimum_cases))
    except (OSError, csv.Error) as exc:
        print("coverage analysis failed", file=sys.stderr)
        return 2
    if args.format == "json":
        print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    else:
        print("# Agent Evaluation Dataset Coverage\n")
        print(f"- Cases: {report['total_cases']} (gate={report['case_count_gate']})")
        print(f"- Scenes: {report['scenes']}")
        print(f"- Routes: {report['routes']}")
        print(f"- Assertion cases: {report['assertion_cases']} ({report['assertion_case_rate']}%)")
        print(f"- Expected-tool cases: {report['expected_tool_cases']}")
        print(f"- Forbidden-tool cases: {report['forbidden_tool_cases']}")
        print(f"- Relevance labeled: {report['relevance_labeled_cases']} ({report['relevance_label_rate']}%)")
        print(f"- Relevance verified: {report['relevance_verified_cases']} ({report['relevance_verified_rate']}%)")
        print(f"- Structural gate: {report['structural_gate']}")
    return 0 if report["case_count_gate"] == "passed" and report["structural_gate"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
