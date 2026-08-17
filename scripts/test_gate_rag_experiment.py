import importlib.util
import unittest
from pathlib import Path


path = Path(__file__).with_name("gate_rag_experiment.py")
spec = importlib.util.spec_from_file_location("gate_rag_experiment", path)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def summary(recall=0.8, mrr=0.8, ndcg=0.8, p95=100.0, verified=100, dataset="dataset"):
    return {
        "latency_ms": {"p95": p95},
        "rag_ranking_verified": {"sample_count": verified, "recall_at_3": recall, "mrr": mrr, "ndcg_at_5": ndcg},
        "evaluation_metadata": {"dataset_sha256": dataset, "model_profile": "model", "embedding_profile": "embedding", "environment": "test"},
    }


class GateRAGExperimentTests(unittest.TestCase):
    def test_passes_non_regressing_comparable_experiment(self):
        report = module.evaluate(summary(), summary(recall=.81, mrr=.8, ndcg=.82, p95=110))
        self.assertTrue(report["passed"])

    def test_rejects_quality_regression_or_insufficient_gold(self):
        self.assertFalse(module.evaluate(summary(), summary(recall=.79))["passed"])
        self.assertFalse(module.evaluate(summary(verified=99), summary(verified=99))["passed"])

    def test_rejects_incomparable_provenance(self):
        report = module.evaluate(summary(), summary(dataset="different"))
        self.assertFalse(report["passed"])
        self.assertEqual(report["comparison_status"], "incomparable")


if __name__ == "__main__":
    unittest.main()
