#!/usr/bin/env python3
"""Safely merge adjudicated RAG Gold labels into an eval CSV.

The command only accepts verified ``expert_review``/``human``/``golden_set``
rows, refuses unknown Case IDs, duplicate Gold rows and conflicting existing
labels, and never invents a relevance label. It prints aggregate counts only.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import sys
from collections import Counter
from pathlib import Path
from typing import Any

ALLOWED_SOURCES = {"human", "expert_review", "golden_set"}
GOLD_FIELDS = {"case_id", "relevant_doc_ids", "relevance_label_source", "relevance_label_status"}


def read_csv(path: Path) -> tuple[list[dict[str, str]], list[str]]:
    with path.open(encoding="utf-8-sig", newline="") as handle:
        reader = csv.DictReader(handle)
        fields = list(reader.fieldnames or [])
        return [{key: str(value or "").strip() for key, value in row.items()} for row in reader], fields


def validate_gold(rows: list[dict[str, str]]) -> list[str]:
    errors: list[str] = []
    seen: set[str] = set()
    for index, row in enumerate(rows, start=2):
        case_id = row.get("case_id", "")
        if not case_id:
            errors.append(f"row {index}: missing case_id")
        if case_id in seen:
            errors.append(f"row {index}: duplicate case_id")
        seen.add(case_id)
        if row.get("relevance_label_status", "").lower() != "verified":
            errors.append(f"row {index}: relevance_label_status must be verified")
        if row.get("relevance_label_source", "").lower() not in ALLOWED_SOURCES:
            errors.append(f"row {index}: unsupported relevance_label_source")
        if "relevant_doc_ids" not in row:
            errors.append(f"row {index}: missing relevant_doc_ids")
    return errors


def merge(dataset: list[dict[str, str]], gold: list[dict[str, str]], replace_existing: bool = False) -> tuple[list[dict[str, str]], dict[str, Any]]:
    errors = validate_gold(gold)
    if errors:
        raise ValueError("; ".join(errors))
    by_case = {(row.get("case_id") or "").strip(): row for row in gold}
    dataset_ids = [row.get("case_id", "").strip() for row in dataset]
    unknown = sorted(set(by_case) - set(dataset_ids))
    if unknown:
        raise ValueError("gold contains Case IDs outside dataset")
    output = []
    applied = 0
    skipped = 0
    for row in dataset:
        case_id = row.get("case_id", "").strip()
        label = by_case.get(case_id)
        if not label:
            output.append(dict(row))
            continue
        existing_status = row.get("relevance_label_status", "").strip().lower()
        existing_docs = row.get("relevant_doc_ids", "").strip()
        incoming_status = label["relevance_label_status"].lower()
        incoming_docs = label.get("relevant_doc_ids", "").strip()
        if existing_status or existing_docs:
            if existing_status == incoming_status and existing_docs == incoming_docs:
                output.append(dict(row))
                skipped += 1
                continue
            if not replace_existing:
                raise ValueError("gold conflicts with an existing dataset label")
        merged = dict(row)
        merged.update({
            "relevant_doc_ids": incoming_docs,
            "relevance_label_source": label["relevance_label_source"],
            "relevance_label_status": label["relevance_label_status"],
        })
        output.append(merged)
        applied += 1
    return output, {
        "dataset_cases": len(dataset),
        "gold_cases": len(gold),
        "applied_cases": applied,
        "already_matching_cases": skipped,
        "verified_cases_after_merge": sum(1 for row in output if row.get("relevance_label_status", "").lower() == "verified"),
    }


def write_csv(path: Path, rows: list[dict[str, str]], fields: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dataset", type=Path, required=True)
    parser.add_argument("--gold", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--summary-json", type=Path)
    parser.add_argument("--replace-existing", action="store_true")
    args = parser.parse_args()
    try:
        dataset, fields = read_csv(args.dataset)
        gold, gold_fields = read_csv(args.gold)
        missing = GOLD_FIELDS - set(gold_fields)
        if missing:
            raise ValueError(f"gold CSV missing columns: {', '.join(sorted(missing))}")
        if "relevant_doc_ids" not in fields or "relevance_label_source" not in fields or "relevance_label_status" not in fields:
            raise ValueError("dataset CSV lacks RAG label columns")
        merged, summary = merge(dataset, gold, args.replace_existing)
        summary["dataset_sha256"] = hashlib.sha256(args.dataset.read_bytes()).hexdigest()
        summary["gold_sha256"] = hashlib.sha256(args.gold.read_bytes()).hexdigest()
        write_csv(args.output, merged, fields)
        if args.summary_json:
            args.summary_json.parent.mkdir(parents=True, exist_ok=True)
            args.summary_json.write_text(json.dumps(summary, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    except (OSError, csv.Error, ValueError) as exc:
        print(f"RAG Gold merge failed: {exc}", file=sys.stderr)
        return 2
    print(json.dumps(summary, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
