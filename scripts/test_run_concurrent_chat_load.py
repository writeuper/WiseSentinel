import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch
from urllib.error import HTTPError


MODULE_PATH = Path(__file__).with_name("run_concurrent_chat_load.py")
SPEC = importlib.util.spec_from_file_location("concurrent_chat_load", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ConcurrentChatLoadTests(unittest.TestCase):

    def test_request_uses_stable_application_code_for_http_error(self) -> None:
        client = MODULE.Client("http://example.invalid/api/v1", "test-key", "default", 1)
        error = HTTPError("http://example.invalid", 503, "busy", {}, None)
        error.read = lambda: json.dumps({"code": 50304, "message": "ignored"}).encode("utf-8")
        with patch.object(MODULE, "urlopen", side_effect=error):
            with self.assertRaisesRegex(RuntimeError, "^api_50304$"):
                client.request("GET", "/chat")

    def test_deleted_session_cleanup_accepts_stable_not_found_code(self) -> None:
        client = object.__new__(MODULE.Client)
        outcomes = iter([
            {"session_id": "session-1"},
            {"trace_id": "trace-1"},
            {},
        ])
        client.request = lambda method, path, payload=None: (
            (_ for _ in ()).throw(RuntimeError("api_40401"))
            if method == "GET" and path.endswith("/messages") else next(outcomes)
        )
        result = client.run_one(1, "safe question")
        self.assertEqual(result["cleanup"], "verified")
    def test_build_summary_reports_all_terminal_outcomes(self) -> None:
        summary = MODULE.build_summary([
            {"outcome": "success", "latency_ms": 100},
            {"outcome": "rate_limited", "latency_ms": 200},
            {"outcome": "success", "latency_ms": 300},
        ])
        self.assertEqual(summary["requests"], 3)
        self.assertEqual(summary["successes"], 2)
        self.assertEqual(summary["success_rate"], 2 / 3)
        self.assertEqual(summary["outcomes"], {"rate_limited": 1, "success": 2})
        self.assertEqual(summary["overload_history"], {})
        self.assertEqual(summary["latency_ms"], {"p50": 200.0, "p95": 300.0, "p99": 300.0, "max": 300.0})
        self.assertEqual(summary["latency_by_outcome"], {
            "rate_limited": {"count": 1, "p50": 200.0, "p95": 200.0, "p99": 200.0, "max": 200.0},
            "success": {"count": 2, "p50": 100.0, "p95": 300.0, "p99": 300.0, "max": 300.0},
        })

    def test_build_summary_separates_admission_rejections_from_success_latency(self) -> None:
        summary = MODULE.build_summary([
            {"outcome": "api_50304", "latency_ms": 90},
            {"outcome": "api_50304", "latency_ms": 110},
            {"outcome": "success", "latency_ms": 9000},
            {"outcome": "success", "latency_ms": 12000},
        ])
        self.assertEqual(summary["latency_by_outcome"]["api_50304"]["count"], 2)
        self.assertEqual(summary["latency_by_outcome"]["api_50304"]["p50"], 90.0)
        self.assertEqual(summary["latency_by_outcome"]["success"]["p50"], 9000.0)

    def test_model_metric_parser_discards_labels(self) -> None:
        metrics = MODULE.parse_model_metric_totals("\n".join([
            'ws_model_calls_total{provider="openai",operation="generate",outcome="success"} 2',
            'ws_model_calls_total{provider="other",operation="stream_complete",outcome="timeout"} 1',
            'ws_model_call_duration_seconds_sum{provider="openai",operation="generate",outcome="success"} 3.5',
            'ws_model_call_duration_seconds_sum{provider="other",operation="stream_complete",outcome="timeout"} 1.25',
            'ws_model_admission_rejections_total{provider="openai"} 4',
            'ws_model_admission_wait_seconds_sum{provider="openai",outcome="accepted"} 0.01',
            'ws_model_admission_wait_seconds_count{provider="openai",outcome="accepted"} 2',
            'ws_model_admission_wait_seconds_sum{provider="openai",outcome="rejected"} 0.002',
            'ws_model_admission_wait_seconds_count{provider="openai",outcome="rejected"} 1',
        ]))
        self.assertEqual(metrics, {"calls": 3.0, "duration_seconds": 4.75, "admission_rejections": 4.0, "admission_wait_accepted_seconds": 0.01, "admission_wait_accepted_count": 2.0, "admission_wait_rejected_seconds": 0.002, "admission_wait_rejected_count": 1.0})
        self.assertEqual(MODULE.metric_delta(metrics, {"calls": 5.0, "duration_seconds": 6.0, "admission_rejections": 7.0, "admission_wait_accepted_seconds": 0.03, "admission_wait_accepted_count": 5.0, "admission_wait_rejected_seconds": 0.004, "admission_wait_rejected_count": 3.0}), {"calls": 2.0, "duration_seconds": 1.25, "admission_rejections": 3.0, "admission_wait_accepted_seconds": 0.019999999999999997, "admission_wait_accepted_count": 3.0, "admission_wait_rejected_seconds": 0.002, "admission_wait_rejected_count": 2.0})

    def test_quality_gate_requires_verified_cleanup_and_explicit_latency_budget(self) -> None:
        summary = {
            "requests": 2,
            "success_rate": 1.0,
            "cleanup": {"verified": 2},
            "outcomes": {"success": 2},
            "model_metrics_delta": {"admission_rejections": 0.0},
            "latency_ms": {"p95": 1500.0},
        }
        self.assertEqual(MODULE.quality_gate(summary, 1.0, 0), [])
        self.assertEqual(MODULE.quality_gate(summary, 1.0, 1000), ["p95_latency"])
        summary["cleanup"] = {"failed": 1, "verified": 1}
        self.assertEqual(MODULE.quality_gate(summary, 1.0, 0), ["cleanup"])
        summary["cleanup"] = {"verified": 2}
        summary["overload_history"] = {"unexpected_messages": 1}
        self.assertEqual(MODULE.quality_gate(summary, 1.0, 0), ["overload_history"])

    def test_quality_gate_detects_admission_attribution_mismatch(self) -> None:
        summary = {
            "requests": 3,
            "success_rate": 1.0,
            "cleanup": {"verified": 3},
            "outcomes": {"success": 2, "api_50304": 1},
            "model_metrics_delta": {"admission_rejections": 0.0},
            "latency_ms": {"p95": 100.0},
        }
        self.assertEqual(MODULE.quality_gate(summary, 0.0, 0), ["admission_attribution"])
