#!/usr/bin/env python3
"""Fail-closed release readiness gate for enterprise Agent evidence artifacts."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


def load_json(path: Path | None) -> dict[str, Any] | None:
    if path is None:
        return None
    try:
        with path.open(encoding="utf-8") as handle:
            value = json.load(handle)
    except (OSError, json.JSONDecodeError):
        return None
    return value if isinstance(value, dict) else None


def check(name: str, passed: bool, reason: str) -> dict[str, Any]:
    return {"name": name, "status": "pass" if passed else "fail", "reason": reason}


def evaluate(
    coverage: dict[str, Any] | None,
    evaluation: dict[str, Any] | None,
    quality: dict[str, Any] | None,
    *,
    minimum_cases: int = 100,
    minimum_verified_rag: int = 100,
    minimum_business_pass_rate: float = 0.0,
    minimum_business_outcome_rate: float = 0.0,
    require_production: bool = False,
    require_slo: bool = False,
    require_business_outcomes: bool = False,
) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    if coverage is None:
        checks.append(check("eval_coverage", False, "coverage artifact missing or invalid"))
    else:
        count = coverage.get("total_cases")
        checks.append(check("eval_case_count", coverage.get("case_count_gate") == "passed" and isinstance(count, int) and count >= minimum_cases, f"cases={count!r}, minimum={minimum_cases}"))
        checks.append(check("eval_structure", coverage.get("structural_gate") == "passed", f"structural_gate={coverage.get('structural_gate')!r}"))
    if evaluation is None:
        checks.append(check("agent_eval", False, "evaluation summary missing or invalid"))
    else:
        verified = ((evaluation.get("rag_ranking_verified") or {}).get("sample_count"))
        pass_rate = evaluation.get("business_pass_rate")
        checks.append(check("business_pass_rate", isinstance(pass_rate, (int, float)) and pass_rate >= minimum_business_pass_rate, f"rate={pass_rate!r}, minimum={minimum_business_pass_rate}"))
        checks.append(check("verified_rag_labels", isinstance(verified, int) and verified >= minimum_verified_rag, f"verified_samples={verified!r}, minimum={minimum_verified_rag}"))
    if quality is None:
        checks.append(check("quality_report", False, "quality aggregate missing or invalid"))
    else:
        traffic = quality.get("traffic_evidence") or {}
        traffic_ok = traffic.get("status") == "production_attested"
        checks.append(check("production_traffic", traffic_ok, f"status={traffic.get('status')!r}")) if require_production else checks.append({"name": "production_traffic", "status": "info", "reason": f"status={traffic.get('status')!r}; production requirement disabled"})
        slo = quality.get("slo") or {}
        slo_ok = slo.get("status") == "within_budget"
        checks.append(check("slo_error_budget", slo_ok, f"status={slo.get('status')!r}")) if require_slo else checks.append({"name": "slo_error_budget", "status": "info", "reason": f"status={slo.get('status')!r}; SLO requirement disabled"})
        outcomes = quality.get("business_outcome_evidence") or {}
        outcome_rate = outcomes.get("success_rate")
        outcome_ok = (
            outcomes.get("status") == "production_attested"
            and isinstance(outcome_rate, (int, float))
            and outcome_rate >= minimum_business_outcome_rate
            and int(outcomes.get("terminal_count") or 0) > 0
        )
        reason = f"status={outcomes.get('status')!r}, success_rate={outcome_rate!r}, minimum={minimum_business_outcome_rate}"
        checks.append(check("business_outcomes", outcome_ok, reason)) if require_business_outcomes else checks.append({"name": "business_outcomes", "status": "info", "reason": reason + "; business outcome requirement disabled"})
    blockers = [item for item in checks if item["status"] == "fail"]
    return {"status": "ready" if not blockers else "not_ready", "checks": checks, "blockers": [item["name"] for item in blockers]}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--coverage", type=Path, required=True)
    parser.add_argument("--evaluation", type=Path, required=True)
    parser.add_argument("--quality", type=Path, required=True)
    parser.add_argument("--minimum-cases", type=int, default=100)
    parser.add_argument("--minimum-verified-rag", type=int, default=100)
    parser.add_argument("--minimum-business-pass-rate", type=float, default=0.0)
    parser.add_argument("--minimum-business-outcome-rate", type=float, default=0.0)
    parser.add_argument("--require-production", action="store_true")
    parser.add_argument("--require-slo", action="store_true")
    parser.add_argument("--require-business-outcomes", action="store_true")
    args = parser.parse_args()
    result = evaluate(load_json(args.coverage), load_json(args.evaluation), load_json(args.quality), minimum_cases=max(1, args.minimum_cases), minimum_verified_rag=max(0, args.minimum_verified_rag), minimum_business_pass_rate=max(0.0, min(1.0, args.minimum_business_pass_rate)), minimum_business_outcome_rate=max(0.0, min(1.0, args.minimum_business_outcome_rate)), require_production=args.require_production, require_slo=args.require_slo, require_business_outcomes=args.require_business_outcomes)
    print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
    return 0 if result["status"] == "ready" else 1


if __name__ == "__main__":
    raise SystemExit(main())
