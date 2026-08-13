import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("report_agent_quality_metrics.py")
SPEC = importlib.util.spec_from_file_location("report_agent_quality_metrics", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class ReportMetricsTests(unittest.TestCase):
    def test_terminal_failure_query_is_not_limited_to_legacy_status_names(self):
        source = MODULE_PATH.read_text(encoding="utf-8")
        self.assertIn("status NOT IN ('success','completed','abandoned')", source)
        self.assertNotIn("status IN ('failed','error','timeout','canceled')", source)

    def test_step_average_uses_finished_trace_left_join_and_zero_step_denominator(self):
        source = MODULE_PATH.read_text(encoding="utf-8")
        self.assertIn("LEFT JOIN ws_agent_trace_step", source)
        self.assertIn("s.tenant_id = t.tenant_id", source)
        self.assertIn("WHERE t.finished_at IS NOT NULL", source)
        self.assertIn('"finished_traces"', source)

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

    def test_parse_rag_inventory_reads_only_global_gauges(self):
        report = MODULE.parse_rag_inventory("""
# HELP ws_rag_active_documents test
ws_rag_active_documents 24
ws_rag_active_published_chunks 78
ws_rag_active_legacy_documents 11
ws_rag_physical_vectors 199
""")
        self.assertEqual(report, {"active_documents": 24, "active_published_chunks": 78,
                                  "active_legacy_documents": 11, "physical_vectors": 199})

    def test_parse_model_histogram_separates_success_and_timeout(self):
        text = """
ws_model_call_duration_seconds_bucket{operation="generate",outcome="success",provider="openai",le="5"} 2
ws_model_call_duration_seconds_bucket{operation="generate",outcome="success",provider="openai",le="10"} 2
ws_model_call_duration_seconds_bucket{operation="generate",outcome="success",provider="openai",le="+Inf"} 2
ws_model_call_duration_seconds_sum{operation="generate",outcome="success",provider="openai"} 8
ws_model_call_duration_seconds_count{operation="generate",outcome="success",provider="openai"} 2
ws_model_call_duration_seconds_bucket{operation="generate",outcome="timeout",provider="openai",le="60"} 1
ws_model_call_duration_seconds_bucket{operation="generate",outcome="timeout",provider="openai",le="+Inf"} 1
ws_model_call_duration_seconds_sum{operation="generate",outcome="timeout",provider="openai"} 60
ws_model_call_duration_seconds_count{operation="generate",outcome="timeout",provider="openai"} 1
"""
        report = MODULE.parse_model_prometheus(text)
        self.assertEqual(report["sample_count"], 3)
        self.assertEqual(report["success_sample_count"], 2)
        self.assertEqual(report["timeout_sample_count"], 1)
        self.assertEqual(report["success_p95_ms"], 5000.0)
        self.assertEqual(report["success_mean_ms"], 4000.0)

    def test_parse_breaker_events_is_bounded_and_aggregated(self):
        report = MODULE.parse_breaker_events("""
ws_model_breaker_events_total{event="open",provider="openai"} 2
ws_model_breaker_events_total{event="rejected",provider="openai"} 5
ws_model_breaker_events_total{event="probe",provider="openai"} 1
ws_model_breaker_events_total{event="closed",provider="openai"} 1
ws_model_breaker_events_total{event="open",provider="other"} 3
ws_model_breaker_events_total{event="unknown",provider="secret"} 99
""")
        self.assertEqual(report, {"open": 5, "rejected": 5, "probe": 1, "closed": 1, "total": 12})

    def test_aggregate_tool_latency_is_grouped_and_payload_free(self):
        report = MODULE.aggregate_tool_latency([
            ["search_logs", "success", "100"],
            ["search_logs", "success", "200"],
            ["search_logs", "error", "900"],
            ["query_metric_range", "success", "50"],
            ["query_metric_range", "completed", "70"],
            ["ignored", "success", "-1"],
        ])
        self.assertEqual(report["tool_count"], 2)
        logs = report["by_tool"][1]
        self.assertEqual(logs["tool_name"], "search_logs")
        self.assertEqual(logs["calls"], 3)
        self.assertEqual(logs["successes"], 2)
        self.assertEqual(logs["success_rate"], 66.67)
        self.assertEqual(logs["zero_latency_samples"], 0)
        self.assertEqual(logs["p50_ms"], 200.0)
        self.assertEqual(logs["p95_ms"], 900.0)

    def test_aggregate_trace_latency_separates_abandoned_and_reports_percentiles(self):
        report = MODULE.aggregate_trace_latency([
            ["success", "100"], ["success", "200"], ["failed", "900"],
            ["abandoned", "5000"], ["invalid", "-1"], ["success", "bad"],
        ])
        self.assertEqual(report["success"]["samples"], 2)
        self.assertEqual(report["success"]["p50_ms"], 100.0)
        self.assertEqual(report["success"]["p95_ms"], 200.0)
        self.assertEqual(report["business_terminal"]["samples"], 3)
        self.assertEqual(report["all_finished"]["samples"], 4)

    def test_tool_latency_reports_zero_sample_quality_signal(self):
        report = MODULE.aggregate_tool_latency([["fast", "success", "0"], ["fast", "error", "2"]])
        self.assertEqual(report["by_tool"][0]["zero_latency_samples"], 1)
        self.assertEqual(report["by_tool"][0]["zero_latency_rate"], 50.0)


if __name__ == "__main__":
    unittest.main()
