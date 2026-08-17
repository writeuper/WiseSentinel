#!/usr/bin/env python3
"""Validate the phase-0 baseline artifacts without third-party dependencies."""
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BASELINE = ROOT / "testdata" / "baseline"
MATRIX = BASELINE / "capability_matrix.yaml"
CASES = sorted(BASELINE.glob("*_case.jsonl"))
VALID_CAPABILITY_STATUS = {"implemented", "partial", "designed", "not_implemented"}
VALID_CASE_STATUS = {"automated", "manual", "blocked"}

def fail(message):
    print(f"ERROR: {message}", file=sys.stderr)
    return 1

def load_json(path):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"{path.relative_to(ROOT)} is not JSON-compatible YAML: {exc}") from exc

def main():
    errors = []
    try:
        matrix = load_json(MATRIX)
    except ValueError as exc:
        return fail(str(exc))
    if matrix.get("schema_version") != "1.0": errors.append("matrix schema_version must be 1.0")
    seen = set()
    for capability in matrix.get("capabilities", []):
        cid, status = capability.get("id"), capability.get("status")
        if not cid or cid in seen: errors.append(f"duplicate or missing capability id: {cid}")
        seen.add(cid)
        if status not in VALID_CAPABILITY_STATUS: errors.append(f"{cid}: invalid status {status}")
        evidence = capability.get("evidence", {})
        if status == "implemented" and (not evidence.get("code") or not evidence.get("tests")):
            errors.append(f"{cid}: implemented capability needs code and test evidence")
        if status != "implemented" and not capability.get("gaps"):
            errors.append(f"{cid}: non-implemented capability needs a gap")
        for path in evidence.get("code", []) + evidence.get("tests", []):
            if not (ROOT / path).is_file(): errors.append(f"{cid}: evidence path missing: {path}")
    if not seen: errors.append("matrix has no capabilities")
    case_ids = set()
    for path in CASES:
        for lineno, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if not line.strip(): continue
            try: case = json.loads(line)
            except json.JSONDecodeError as exc:
                errors.append(f"{path.name}:{lineno}: invalid JSON: {exc.msg}"); continue
            cid = case.get("case_id")
            if not cid or cid in case_ids: errors.append(f"{path.name}:{lineno}: duplicate or missing case_id {cid}")
            case_ids.add(cid)
            if case.get("status") not in VALID_CASE_STATUS: errors.append(f"{cid}: invalid case status")
            if case.get("status") in {"manual", "blocked"} and not case.get("gap"): errors.append(f"{cid}: manual/blocked case needs a gap")
            expected = case.get("expected", {})
            if not expected.get("forbidden"): errors.append(f"{cid}: expected.forbidden (negative assertion) is required")
            for test in case.get("verification", {}).get("tests", []):
                if not (ROOT / test).is_file(): errors.append(f"{cid}: test path missing: {test}")
    if not CASES: errors.append("no baseline case files found")
    if errors:
        for error in errors: print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print(f"baseline valid: {len(seen)} capabilities, {len(case_ids)} cases, {len(CASES)} case files")
    return 0

if __name__ == "__main__": sys.exit(main())
