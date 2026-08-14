import importlib.util
import pathlib
import json
import tempfile
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

    def test_parse_model_admission_reports_capacity_and_wait_signals(self):
        report = MODULE.parse_model_admission("""
ws_model_admission_rejections_total{provider="openai"} 4
ws_model_admission_rejections_total{provider="other"} 1
ws_model_admission_in_flight{provider="openai"} 2
ws_model_admission_in_flight{provider="other"} 1
ws_model_admission_wait_seconds_sum{provider="openai",outcome="accepted"} 0.006
ws_model_admission_wait_seconds_count{provider="openai",outcome="accepted"} 3
ws_model_admission_wait_seconds_sum{provider="openai",outcome="rejected"} 0.002
ws_model_admission_wait_seconds_count{provider="openai",outcome="rejected"} 2
""")
        self.assertEqual(report["rejections"], 5)
        self.assertEqual(report["in_flight"], 3)
        self.assertEqual(report["wait_accepted_count"], 3)
        self.assertEqual(report["wait_rejected_count"], 2)
        self.assertEqual(report["rejection_rate"], 40.0)
        self.assertEqual(report["mean_wait_accepted_ms"], 2.0)
        self.assertEqual(report["mean_wait_rejected_ms"], 1.0)

    def test_parse_model_tokens_aggregates_bounded_usage(self):
        report = MODULE.parse_model_tokens("""
ws_model_tokens_total{operation="generate",provider="openai",token_type="prompt"} 100
ws_model_tokens_total{operation="generate",provider="openai",token_type="completion"} 40
ws_model_tokens_total{operation="generate",provider="openai",token_type="total"} 140
ws_model_tokens_total{operation="stream_complete",provider="other",token_type="total"} 60
ws_model_tokens_total{operation="generate",provider="secret",token_type="unknown"} 999
""")
        self.assertEqual(report["prompt_tokens"], 100)
        self.assertEqual(report["completion_tokens"], 40)
        self.assertEqual(report["total_tokens"], 200)
        self.assertEqual(report["operations"], {"generate": 280, "stream_complete": 60})
        self.assertEqual(report["input_output_ratio"], 2.5)
        self.assertEqual(report["usage_samples"], 1)

    def test_pricing_profile_is_fail_closed_when_missing_or_invalid(self):
        self.assertEqual(MODULE.load_pricing_profile("")["status"], "not_claimed")
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "pricing.json"
            path.write_text(json.dumps({"profile": "bad profile", "currency": "USD", "prompt_usd_per_1k": 1, "completion_usd_per_1k": 2}), encoding="utf-8")
            self.assertEqual(MODULE.load_pricing_profile(str(path))["status"], "invalid")

    def test_model_cost_estimate_uses_prompt_and_completion_rates(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "pricing.json"
            path.write_text(json.dumps({"profile": "model-v1", "currency": "USD", "prompt_usd_per_1k": 1.5, "completion_usd_per_1k": 3.0}), encoding="utf-8")
            pricing = MODULE.load_pricing_profile(str(path))
        cost = MODULE.estimate_model_cost({"prompt_tokens": 2000, "completion_tokens": 500}, pricing, 5)
        self.assertEqual(cost["status"], "estimated")
        self.assertEqual(cost["prompt_cost"], 3.0)
        self.assertEqual(cost["completion_cost"], 1.5)
        self.assertEqual(cost["total_cost"], 4.5)
        self.assertEqual(cost["avg_cost_per_generation"], 0.9)
        self.assertEqual(len(cost["pricing_fingerprint"]), 64)

    def test_model_cost_estimate_is_na_without_prices(self):
        cost = MODULE.estimate_model_cost({"prompt_tokens": 100}, {"status": "not_claimed"}, 1)
        self.assertEqual(cost["status"], "not_claimed")
        self.assertNotIn("total_cost", cost)

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
        self.assertEqual(logs["outcomes"]["success"], 2)
        self.assertEqual(logs["outcomes"]["error"], 1)
        self.assertEqual(logs["dependency_availability_rate"], 66.67)
        self.assertEqual(logs["dependency_response_rate"], 100.0)
        self.assertEqual(logs["unavailable_rate"], 0.0)
        self.assertEqual(logs["zero_latency_samples"], 0)
        self.assertEqual(logs["p50_ms"], 200.0)
        self.assertEqual(logs["p95_ms"], 900.0)

    def test_aggregate_index_tasks_separates_failed_and_retryable_states(self):
        report = MODULE.aggregate_index_tasks([
            ["success", "", "4"], ["failed", "Milvus DeadlineExceeded", "2"], ["failed", "document deleted", "1"], ["failed", "embedding HTTP 403 quota", "2"], ["failed", "embedding HTTP 404 not found", "1"], ["retry_wait", "", "1"], ["running", "", "1"],
        ])
        self.assertEqual(report["total"], 12)
        self.assertEqual(report["failed_or_dead"], 6)
        self.assertEqual(report["retryable_or_running"], 2)
        self.assertEqual(report["failure_rate"], 50.0)
        self.assertEqual(report["failure_categories"], {"document_deleted": 1, "embedding_not_found": 1, "embedding_quota": 2, "milvus_deadline": 2})

    def test_aggregate_ops_tasks_reports_completion_failure_inflight_and_latency(self):
        report = MODULE.aggregate_ops_tasks([
            ["success", "3", "3", "900"],
            ["failed", "1", "1", "500"],
            ["timeout", "1", "1", "700"],
            ["running", "2", "0", "0"],
            ["retrying", "1", "0", "0"],
            ["unexpected-secret-status", "1", "1", "100"],
        ])
        self.assertEqual(report["total"], 9)
        self.assertEqual(report["finished"], 6)
        self.assertEqual(report["successful"], 3)
        self.assertEqual(report["failed_or_timeout"], 2)
        self.assertEqual(report["in_flight"], 3)
        self.assertEqual(report["completion_rate"], 50.0)
        self.assertEqual(report["failure_rate"], 33.33)
        self.assertEqual(report["avg_e2e_latency_ms"], 366.667)
        self.assertIn("other", report["by_status"])

    def test_aggregate_ops_tasks_without_finished_rows_is_explicitly_unavailable(self):
        report = MODULE.aggregate_ops_tasks([["running", "2", "0", "0"]])
        self.assertIsNone(report["completion_rate"])
        self.assertIsNone(report["failure_rate"])
        self.assertIsNone(report["avg_e2e_latency_ms"])
        self.assertEqual(report["in_flight"], 2)

    def test_aggregate_approvals_reports_decision_rate_and_latency(self):
        report = MODULE.aggregate_approvals([
            ["pending", ""], ["approved", "100"], ["rejected", "200"],
            ["expired", "500"], ["unexpected", "bad"],
        ])
        self.assertEqual(report["total"], 5)
        self.assertEqual(report["decided"], 4)
        self.assertEqual(report["pending"], 1)
        self.assertEqual(report["approved"], 1)
        self.assertEqual(report["decision_rate"], 80.0)
        self.assertEqual(report["decision_latency_samples"], 3)
        self.assertEqual(report["decision_latency_p50_ms"], 200.0)
        self.assertEqual(report["decision_latency_p95_ms"], 500.0)

    def test_aggregate_approvals_is_explicitly_unavailable_without_decisions(self):
        report = MODULE.aggregate_approvals([["pending", ""]])
        self.assertEqual(report["decision_rate"], 0.0)
        self.assertIsNone(report["decision_latency_p95_ms"])

    def test_aggregate_feedback_separates_ratings_and_target_types(self):
        report = MODULE.aggregate_feedback([
            ["answer", "useful"], ["answer", "bad"],
            ["fault_knowledge", "useful"], ["tool_call", "unknown"],
        ])
        self.assertEqual(report["total"], 4)
        self.assertEqual(report["rated"], 3)
        self.assertEqual(report["useful"], 2)
        self.assertEqual(report["bad"], 1)
        self.assertEqual(report["other"], 1)
        self.assertEqual(report["useful_rate"], 66.67)
        self.assertEqual(report["by_target"]["answer"]["bad"], 1)

    def test_aggregate_feedback_is_na_without_ratings(self):
        report = MODULE.aggregate_feedback([["answer", "other"]])
        self.assertEqual(report["total"], 1)
        self.assertIsNone(report["useful_rate"])

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

    def test_tool_dependency_availability_excludes_governance_rejections(self):
        report = MODULE.aggregate_tool_latency([
            ["tool", "success", "10"], ["tool", "unavailable", "0"],
            ["tool", "timeout", "100"], ["tool", "rejected", "0"],
        ])
        self.assertEqual(report["dependency_attempts"], 3)
        self.assertEqual(report["dependency_available"], 1)
        self.assertEqual(report["dependency_availability_rate"], 33.33)
        self.assertEqual(report["dependency_response_rate"], 33.33)
        self.assertEqual(report["by_tool"][0]["timeout_rate"], 33.33)

    def test_traffic_attestation_is_not_claimed_when_missing(self):
        report = MODULE.load_traffic_attestation("")
        self.assertEqual(report, {"status": "not_claimed", "source": "no_attestation_configured"})

    def test_valid_production_attestation_exposes_only_aggregate_fields(self):
        payload = {
            "status": "production_attested", "source": "gateway_aggregate",
            "window_start": "2026-08-14T00:00:00Z", "window_end": "2026-08-14T01:00:00Z",
            "request_count": 1000, "tenant_count": 12, "user_count": 80,
            "attestation_fingerprint": "a" * 64, "raw_tenant_ids": ["must-not-leak"],
        }
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "traffic.json"
            path.write_text(json.dumps(payload), encoding="utf-8")
            report = MODULE.load_traffic_attestation(str(path))
        self.assertTrue(report["attested"])
        self.assertEqual(report["request_count"], 1000)
        self.assertNotIn("raw_tenant_ids", report)

    def test_invalid_attestation_fails_closed_for_fingerprint_window_and_counts(self):
        cases = [
            {"status": "production_attested", "source": "gateway_aggregate", "attestation_fingerprint": "short"},
            {"status": "staging", "source": "load_test", "window_start": "2026-08-14T01:00:00Z", "window_end": "2026-08-14T00:00:00Z", "request_count": 1, "tenant_count": 1, "user_count": 1},
            {"status": "staging", "source": "load_test", "window_start": "2026-08-14T00:00:00Z", "window_end": "2026-08-14T01:00:00Z", "request_count": 1, "tenant_count": 2, "user_count": 1},
        ]
        with tempfile.TemporaryDirectory() as directory:
            for index, payload in enumerate(cases):
                path = pathlib.Path(directory) / f"traffic-{index}.json"
                path.write_text(json.dumps(payload), encoding="utf-8")
                self.assertEqual(MODULE.load_traffic_attestation(str(path))["status"], "invalid")


if __name__ == "__main__":
    unittest.main()
