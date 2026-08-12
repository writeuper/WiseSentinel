import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("report_agent_quality_metrics.py")
SPEC = importlib.util.spec_from_file_location("report_agent_quality_metrics", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class ReportMetricsTests(unittest.TestCase):
    def test_parse_histogram_separates_success_and_error(self):
        text = """
ws_rag_retrieval_duration_seconds_bucket{outcome="success",confidence="high",le="0.25"} 3
ws_rag_retrieval_duration_seconds_bucket{outcome="success",confidence="high",le="0.5"} 3
ws_rag_retrieval_duration_seconds_bucket{outcome="success",confidence="high",le="+Inf"} 3
ws_rag_retrieval_duration_seconds_sum{outcome="success",confidence="high"} 0.45
ws_rag_retrieval_duration_seconds_count{outcome="success",confidence="high"} 3
ws_rag_retrieval_duration_seconds_bucket{outcome="error",confidence="low",le="15"} 0
ws_rag_retrieval_duration_seconds_bucket{outcome="error",confidence="low",le="+Inf"} 2
ws_rag_retrieval_duration_seconds_sum{outcome="error",confidence="low"} 40
ws_rag_retrieval_duration_seconds_count{outcome="error",confidence="low"} 2
"""
        report = MODULE.parse_prometheus(text)
        self.assertEqual(report["sample_count"], 5)
        self.assertEqual(report["success_sample_count"], 3)
        self.assertEqual(report["error_sample_count"], 2)
        self.assertEqual(report["success_p95_ms"], 250.0)
        self.assertEqual(report["success_mean_ms"], 150.0)
        self.assertEqual(report["p95_semantics"], "histogram_bucket_upper_bound_ms")

    def test_empty_metrics_are_explicitly_unavailable(self):
        report = MODULE.parse_prometheus("")
        self.assertEqual(report["sample_count"], 0)
        self.assertIsNone(report["success_p95_ms"])


if __name__ == "__main__":
    unittest.main()
