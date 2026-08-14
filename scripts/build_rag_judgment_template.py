#!/usr/bin/env python3
"""Prepare an explicitly unlabeled RAG relevance-review template.

The template is a workflow artifact for two human/expert reviewers.  It never
guesses relevance: every emitted row has a blank ``relevance`` value and a
``draft`` status, so it cannot accidentally be consumed as a verified Gold
set by ``validate_rag_judgments.py``.  Retrieved candidates are limited to the
top K IDs recorded by an evaluation run; reviewers may add additional corpus
IDs before adjudication when measuring recall.
"""

from __future__ import annotations

import argparse
import csv
import json
import sys
from pathlib import Path
from typing import Any


FIELDS = [
    "case_id", "annotator_id", "doc_id", "relevance", "review_status",
    "candidate_rank", "candidate_source",
]


def split_ids(value: Any) -> list[str]:
    values = value if isinstance(value, list) else str(value or "").split("|")
    result: list[str] = []
    seen: set[str] = set()
    for item in values:
        candidate = str(item or "").strip()
        if candidate and candidate not in seen:
            result.append(candidate)
            seen.add(candidate)
    return result


def is_rag_case(row: dict[str, str]) -> bool:
    scene = (row.get("scene") or "").strip().lower()
    tools = set(split_ids(row.get("expected_tools")))
    return scene.startswith("chat-rag") or "query_internal_docs" in tools or bool((row.get("expected_knowledge") or "").strip())


def build_template_rows(
    cases: list[dict[str, str]],
    annotators: list[str],
    top_k: int = 5,
) -> tuple[list[dict[str, str]], dict[str, int]]:
    if len(set(annotators)) < 2:
        raise ValueError("at least two distinct annotator IDs are required")
    if top_k < 1:
        raise ValueError("top_k must be positive")
    rows: list[dict[str, str]] = []
    rag_cases = 0
    missing_retrieval = 0
    for case in cases:
        if not is_rag_case(case):
            continue
        rag_cases += 1
        candidates = split_ids(case.get("retrieved_doc_ids"))[:top_k]
        if not candidates:
            missing_retrieval += 1
            continue
        case_id = (case.get("case_id") or "").strip()
        for annotator in annotators:
            for rank, doc_id in enumerate(candidates, start=1):
                rows.append({
                    "case_id": case_id,
                    "annotator_id": annotator,
                    "doc_id": doc_id,
                    "relevance": "",
                    "review_status": "draft",
                    "candidate_rank": str(rank),
                    "candidate_source": "evaluation_retrieved_top_k",
                })
    return rows, {
        "rag_cases": rag_cases,
        "cases_with_candidates": rag_cases - missing_retrieval,
        "cases_missing_retrieval": missing_retrieval,
        "annotator_count": len(set(annotators)),
        "candidate_rows": len(rows),
        "labeled_rows": 0,
        "verified_rows": 0,
    }


def read_cases(path: Path) -> list[dict[str, str]]:
    with path.open(encoding="utf-8-sig", newline="") as handle:
        return [dict(row) for row in csv.DictReader(handle)]


def write_rows(path: Path, rows: list[dict[str, str]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=FIELDS)
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evaluation", type=Path, required=True, help="Evaluation CSV containing retrieved_doc_ids.")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--annotator", action="append", required=True, help="Repeat for each reviewer; at least two distinct IDs are required.")
    parser.add_argument("--top-k", type=int, default=5)
    parser.add_argument("--summary-json", type=Path)
    args = parser.parse_args()
    try:
        rows, summary = build_template_rows(read_cases(args.evaluation), args.annotator, args.top_k)
        write_rows(args.output, rows)
        if args.summary_json:
            args.summary_json.parent.mkdir(parents=True, exist_ok=True)
            args.summary_json.write_text(json.dumps(summary, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    except (OSError, csv.Error, ValueError) as exc:
        print(f"RAG judgment template failed: {exc}", file=sys.stderr)
        return 2
    print(json.dumps(summary, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
