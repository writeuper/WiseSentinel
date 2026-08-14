#!/usr/bin/env python3
"""Fail-closed release readiness gate for enterprise Agent evidence artifacts."""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
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


def artifact_timestamp(artifact: dict[str, Any] | None, evaluation: bool = False) -> str | None:
    if not artifact:
        return None
    if evaluation:
        metadata = artifact.get("evaluation_metadata") or {}
        if isinstance(metadata, dict) and metadata.get("generated_at"):
            return str(metadata.get("generated_at"))
    value = artifact.get("generated_at")
    return str(value) if value else None


def parse_timestamp(value: str | None) -> datetime | None:
    if not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed if parsed.tzinfo else None


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
    require_provenance: bool = False,
    max_artifact_age_hours: float | None = None,
    now: datetime | None = None,
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
    provenance_required = require_provenance or max_artifact_age_hours is not None
    if provenance_required:
        coverage_hash = coverage.get("dataset_sha256") if coverage else None
        evaluation_metadata = (evaluation or {}).get("evaluation_metadata") if evaluation else None
        evaluation_hash = evaluation_metadata.get("dataset_sha256") if isinstance(evaluation_metadata, dict) else None
        timestamps = {
            "coverage": artifact_timestamp(coverage),
            "evaluation": artifact_timestamp(evaluation, evaluation=True),
            "quality": artifact_timestamp(quality),
        }
        provenance_ok = all(
            isinstance(artifact_hash, str) and artifact_hash and artifact_hash != "unavailable"
            for artifact_hash in (coverage_hash, evaluation_hash)
        ) and isinstance(evaluation_metadata, dict) and bool(evaluation_metadata.get("git_commit")) and all(timestamps.values())
        checks.append(check("evidence_provenance", provenance_ok, f"coverage_hash={bool(coverage_hash)}, evaluation_hash={bool(evaluation_hash)}, timestamps={sum(bool(value) for value in timestamps.values())}/3")) if require_provenance else checks.append({"name": "evidence_provenance", "status": "info", "reason": "provenance requirement disabled"})
        consistency_ok = bool(coverage_hash and evaluation_hash and coverage_hash == evaluation_hash)
        checks.append(check("dataset_consistency", consistency_ok, f"coverage_hash_matches_evaluation={consistency_ok}")) if require_provenance else checks.append({"name": "dataset_consistency", "status": "info", "reason": "provenance requirement disabled"})
        if max_artifact_age_hours is not None:
            reference = now or datetime.now(timezone.utc)
            age_errors: list[str] = []
            for name, value in timestamps.items():
                parsed = parse_timestamp(value)
                if parsed is None:
                    age_errors.append(f"{name}:invalid_timestamp")
                    continue
                age_hours = (reference - parsed).total_seconds() / 3600
                if age_hours < -0.01 or age_hours > max_artifact_age_hours:
                    age_errors.append(f"{name}:{round(age_hours, 3)}h")
            checks.append(check("evidence_freshness", not age_errors, f"max_age_hours={max_artifact_age_hours}, errors={age_errors}"))
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
    parser.add_argument("--require-provenance", action="store_true")
    parser.add_argument("--max-artifact-age-hours", type=float)
    args = parser.parse_args()
    if args.max_artifact_age_hours is not None and args.max_artifact_age_hours <= 0:
        parser.error("--max-artifact-age-hours must be positive")
    result = evaluate(load_json(args.coverage), load_json(args.evaluation), load_json(args.quality), minimum_cases=max(1, args.minimum_cases), minimum_verified_rag=max(0, args.minimum_verified_rag), minimum_business_pass_rate=max(0.0, min(1.0, args.minimum_business_pass_rate)), minimum_business_outcome_rate=max(0.0, min(1.0, args.minimum_business_outcome_rate)), require_production=args.require_production, require_slo=args.require_slo, require_business_outcomes=args.require_business_outcomes, require_provenance=args.require_provenance, max_artifact_age_hours=args.max_artifact_age_hours)
    print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
    return 0 if result["status"] == "ready" else 1


if __name__ == "__main__":
    raise SystemExit(main())
