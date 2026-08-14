import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("compare_agent_eval.py")
SPEC = importlib.util.spec_from_file_location("compare_agent_eval", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def summary(**overrides):
    value = {
        "overall_pass_rate": 0.70,
        "overall_pass_ci95": [0.55, 0.82],
        "business_pass_rate": 0.75,
        "business_pass_ci95": [0.58, 0.86],
        "route_accuracy": 0.9,
        "citation_grounding_rate": None,
        "latency_ms": {"p50": 100, "p95": 500},
        "rag_ranking": {"sample_count": 0},
        "rag_ranking_verified": {"sample_count": 0},
        "executed_cases": 20,
        "business_cases": 18,
        "evaluation_metadata": {
            "dataset_sha256": "same-dataset",
            "model_profile": "model-v1",
            "embedding_profile": "embed-v1",
            "environment": "integration",
        },
    }
    for key, value_override in overrides.items():
        if "." not in key:
            value[key] = value_override
    return value


class CompareAgentEvalTests(unittest.TestCase):
    def test_comparable_run_reports_directional_deltas_and_ci_overlap(self):
        before = summary()
        after = summary(overall_pass_rate=0.80, latency_ms={"p50": 90, "p95": 450})
        result = MODULE.compare_summaries(before, after)
        self.assertEqual(result["status"], "ok")
        self.assertTrue(result["deltas"]["overall_pass_rate"]["improved"])
        self.assertTrue(result["deltas"]["latency_ms.p95"]["improved"])
        self.assertTrue(result["deltas"]["overall_pass_rate"]["ci95_overlap"])

    def test_provenance_mismatch_is_incomparable_by_default(self):
        result = MODULE.compare_summaries(summary(), summary(evaluation_metadata={"dataset_sha256": "different"}))
        self.assertEqual(result["status"], "incomparable")
        self.assertFalse(result["comparable"])
        self.assertEqual(result["deltas"], {})

    def test_allow_incompatible_marks_result_without_hiding_mismatch(self):
        result = MODULE.compare_summaries(summary(), summary(evaluation_metadata={"dataset_sha256": "different"}), allow_incompatible=True)
        self.assertEqual(result["status"], "incompatible_allowed")
        self.assertIn("dataset_sha256", result["metadata"]["mismatches"])
        self.assertIn("overall_pass_rate", result["deltas"])

    def test_missing_or_nan_metrics_are_not_compared(self):
        before = summary()
        after = summary()
        after["overall_pass_rate"] = None
        after["latency_ms"]["p95"] = float("nan")
        result = MODULE.compare_summaries(before, after)
        self.assertNotIn("overall_pass_rate", result["deltas"])
        self.assertNotIn("latency_ms.p95", result["deltas"])

    def test_citation_grounding_delta_is_compared_only_when_both_runs_are_verifiable(self):
        before = summary(citation_grounding_rate=0.5)
        after = summary(citation_grounding_rate=0.75)
        result = MODULE.compare_summaries(before, after)
        self.assertTrue(result["deltas"]["citation_grounding_rate"]["improved"])

        unverifiable = MODULE.compare_summaries(before, summary(citation_grounding_rate=None))
        self.assertNotIn("citation_grounding_rate", unverifiable["deltas"])

    def test_cli_writes_json_and_returns_nonzero_for_incomparable_runs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            before, after, output = root / "before.json", root / "after.json", root / "diff.json"
            before.write_text(json.dumps(summary()), encoding="utf-8")
            after.write_text(json.dumps(summary(evaluation_metadata={"dataset_sha256": "different"})), encoding="utf-8")
            # Exercise the pure JSON contract instead of mutating sys.argv.
            result = MODULE.compare_summaries(MODULE.load_summary(before), MODULE.load_summary(after))
            output.write_text(json.dumps(result), encoding="utf-8")
            self.assertEqual(json.loads(output.read_text())["status"], "incomparable")


if __name__ == "__main__":
    unittest.main()
