import importlib.util
import unittest
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
    value = {"traffic_evidence": {"status": "production_attested"}, "slo": {"status": "within_budget"}}
    value.update(overrides)
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


if __name__ == "__main__":
    unittest.main()
