import importlib.util
import unittest
from datetime import datetime, timezone
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("check_agent_release_readiness.py")
SPEC = importlib.util.spec_from_file_location("release_readiness", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def coverage(**overrides):
    value = {"total_cases": 130, "case_count_gate": "passed", "structural_gate": "passed"}
    value.update(overrides)
    return value


def evaluation(**overrides):
    value = {"business_pass_rate": 0.9, "rag_ranking_verified": {"sample_count": 100}}
    value.update(overrides)
    return value


def quality(**overrides):
    value = {
        "traffic_evidence": {"status": "production_attested"},
        "slo": {"status": "within_budget"},
        "business_outcome_evidence": {"status": "production_attested", "terminal_count": 100, "success_rate": 0.95},
    }
    value.update(overrides)
    return value


def provenance_coverage():
    value = coverage(dataset_sha256="a" * 64, generated_at="2026-08-14T00:00:00Z")
    return value


def provenance_evaluation():
    value = evaluation(evaluation_metadata={
        "dataset_sha256": "a" * 64,
        "git_commit": "abc123",
        "generated_at": "2026-08-14T00:30:00Z",
    })
    return value


def provenance_quality():
    value = quality(generated_at="2026-08-14T00:45:00Z")
    return value


class ReleaseReadinessTests(unittest.TestCase):
    def test_ready_requires_all_evidence_when_production_and_slo_are_required(self):
        result = MODULE.evaluate(coverage(), evaluation(), quality(), require_production=True, require_slo=True, minimum_business_pass_rate=0.8)
        self.assertEqual(result["status"], "ready")
        self.assertEqual(result["blockers"], [])

    def test_missing_verified_rag_and_production_are_blockers(self):
        result = MODULE.evaluate(coverage(), evaluation(rag_ranking_verified={"sample_count": 0}), quality(traffic_evidence={"status": "not_claimed"}), require_production=True, minimum_verified_rag=100)
        self.assertEqual(result["status"], "not_ready")
        self.assertIn("verified_rag_labels", result["blockers"])
        self.assertIn("production_traffic", result["blockers"])

    def test_disabled_optional_requirements_are_info_not_pass(self):
        result = MODULE.evaluate(coverage(), evaluation(), quality(traffic_evidence={"status": "not_claimed"}, slo={"status": "not_claimed"}))
        self.assertEqual(result["status"], "ready")
        info = {item["name"]: item for item in result["checks"] if item["status"] == "info"}
        self.assertIn("production_traffic", info)
        self.assertIn("slo_error_budget", info)

    def test_missing_artifact_fails_closed(self):
        result = MODULE.evaluate(None, evaluation(), quality())
        self.assertEqual(result["status"], "not_ready")
        self.assertIn("eval_coverage", result["blockers"])

    def test_business_outcomes_are_required_only_when_explicitly_enabled(self):
        missing = quality(business_outcome_evidence={"status": "not_claimed"})
        optional = MODULE.evaluate(coverage(), evaluation(), missing)
        self.assertEqual(optional["status"], "ready")
        required = MODULE.evaluate(coverage(), evaluation(), missing, require_business_outcomes=True)
        self.assertEqual(required["status"], "not_ready")
        self.assertIn("business_outcomes", required["blockers"])

    def test_business_outcome_rate_threshold_is_enforced(self):
        result = MODULE.evaluate(coverage(), evaluation(), quality(), require_business_outcomes=True, minimum_business_outcome_rate=0.99)
        self.assertEqual(result["status"], "not_ready")
        self.assertIn("business_outcomes", result["blockers"])

    def test_provenance_requires_matching_dataset_and_fresh_artifacts(self):
        result = MODULE.evaluate(
            provenance_coverage(), provenance_evaluation(), provenance_quality(),
            require_provenance=True, max_artifact_age_hours=2,
            now=datetime(2026, 8, 14, 1, 30, tzinfo=timezone.utc),
        )
        self.assertEqual(result["status"], "ready")
        self.assertNotIn("evidence_provenance", result["blockers"])
        self.assertNotIn("dataset_consistency", result["blockers"])
        self.assertNotIn("evidence_freshness", result["blockers"])

    def test_provenance_rejects_dataset_mismatch_and_stale_artifact(self):
        evaluation_data = provenance_evaluation()
        evaluation_data["evaluation_metadata"]["dataset_sha256"] = "b" * 64
        coverage_data = provenance_coverage()
        coverage_data["generated_at"] = "2026-08-10T00:00:00Z"
        result = MODULE.evaluate(
            coverage_data, evaluation_data, provenance_quality(),
            require_provenance=True, max_artifact_age_hours=2,
            now=datetime(2026, 8, 14, 1, 30, tzinfo=timezone.utc),
        )
        self.assertEqual(result["status"], "not_ready")
        self.assertIn("dataset_consistency", result["blockers"])
        self.assertIn("evidence_freshness", result["blockers"])


if __name__ == "__main__":
    unittest.main()
