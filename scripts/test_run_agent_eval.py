import importlib.util
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest


MODULE_PATH = Path(__file__).with_name("run_agent_eval.py")
SPEC = importlib.util.spec_from_file_location("agent_eval", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class EvalSessionCleanupTests(unittest.TestCase):
    def test_evaluation_metadata_contains_provenance_without_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "cases.csv"
            path.write_text("case_id,input\nC-1,hello\n", encoding="utf-8")
            metadata = MODULE.evaluation_metadata(path, SimpleNamespace(
                environment="staging", run_label="candidate-a", model_profile="model-v2",
                embedding_profile="embed-v1", only="chat", case="", limit=10,
            ), "tenant-a")
        self.assertEqual(metadata["environment"], "staging")
        self.assertEqual(len(metadata["dataset_sha256"]), 64)
        self.assertEqual(len(metadata["tenant_fingerprint"]), 12)
        self.assertNotIn("api_key", str(metadata).lower())

    def test_wilson_interval_reflects_small_sample_uncertainty(self) -> None:
        lower, upper = MODULE.wilson_interval(1, 1)
        self.assertLess(lower, 1.0)
        self.assertEqual(upper, 1.0)
        self.assertEqual(MODULE.wilson_interval(0, 0), (None, None))

    def test_metrics_include_confidence_intervals(self) -> None:
        metrics = MODULE.build_metrics([
            {"actual_route": "chat", "expected_route": "chat", "passed": "Y", "bad_case": ""},
            {"actual_route": "chat", "expected_route": "chat", "passed": "N", "bad_case": "keyword_miss"},
        ])
        self.assertEqual(metrics["overall_pass_ci95"], metrics["business_pass_ci95"])
        self.assertEqual(len(metrics["overall_pass_ci95"]), 2)

    def test_ranking_metrics_calculate_recall_mrr_and_ndcg(self) -> None:
        metrics = MODULE.ranking_metrics("doc-a|doc-b", "doc-x|doc-b|doc-a")
        self.assertEqual(metrics["recall_at_1"], 0.0)
        self.assertEqual(metrics["recall_at_3"], 1.0)
        self.assertAlmostEqual(metrics["mrr"], 0.5)
        self.assertGreater(metrics["ndcg_at_5"], 0.69)

    def test_ranking_report_refuses_unlabelled_rows(self) -> None:
        report = MODULE.build_rag_ranking_metrics([{"retrieved_doc_ids": "doc-a"}, {"relevant_doc_ids": "doc-a"}])
        self.assertEqual(report["sample_count"], 0)
        self.assertIsNone(report["recall_at_5"])

    def test_ranking_report_aggregates_only_labelled_rows(self) -> None:
        report = MODULE.build_rag_ranking_metrics([
            {"relevant_doc_ids": "doc-a", "retrieved_doc_ids": "doc-a|doc-b"},
            {"relevant_doc_ids": "doc-a|doc-b", "retrieved_doc_ids": "doc-x|doc-b"},
        ])
        self.assertEqual(report["sample_count"], 2)
        self.assertEqual(report["recall_at_1"], 0.5)
        self.assertEqual(report["recall_at_5"], 0.75)

    def test_relevance_provenance_separates_verified_unverified_and_unlabeled(self) -> None:
        rows = [
            {"relevant_doc_ids": "doc-a", "relevance_label_source": "expert_review", "relevance_label_status": "verified"},
            {"relevant_doc_ids": "doc-b", "relevance_label_source": "model_seed", "relevance_label_status": "unverified"},
            {"relevant_doc_ids": "", "relevance_label_source": "", "relevance_label_status": ""},
        ]
        self.assertEqual(MODULE.relevance_label_summary(rows), {"verified": 1, "unverified": 1, "unlabeled": 1})

    def test_strict_ranking_excludes_unverified_labels(self) -> None:
        rows = [
            {"relevant_doc_ids": "doc-a", "retrieved_doc_ids": "doc-a", "relevance_label_source": "model_seed", "relevance_label_status": "unverified"},
            {"relevant_doc_ids": "doc-b", "retrieved_doc_ids": "doc-b", "relevance_label_source": "human", "relevance_label_status": "verified"},
        ]
        report = MODULE.build_rag_ranking_metrics(rows, require_verified=True)
        self.assertEqual(report["sample_count"], 1)
        self.assertTrue(report["require_verified"])

    def test_strict_relevance_gate_requires_minimum_reviewed_samples(self) -> None:
        rows = [{"relevant_doc_ids": "doc-a", "relevance_label_source": "human", "relevance_label_status": "verified"}]
        with self.assertRaisesRegex(ValueError, "requires at least 2 verified samples"):
            MODULE.validate_relevance_dataset(rows, minimum_verified=2)
        self.assertEqual(MODULE.validate_relevance_dataset(rows, minimum_verified=1), (1, 0))

    def test_citation_quality_requires_structural_fields_and_counts_versions(self) -> None:
        self.assertEqual(MODULE.citation_quality([{"doc_id":"d", "chunk_id":"c", "source":"s", "snippet":"x", "version":"v1"}, {"doc_id":"d"}]), (2, 1, 1))
        self.assertEqual(MODULE.citation_quality("not-a-list"), (0, 0, 0))

    def test_citation_grounding_uses_independent_trace_evidence(self) -> None:
        trace = {"steps": [{"step_type": "rag", "evidence_doc_ids": ["doc-a"]}]}
        evidence = MODULE.extract_trace_evidence_doc_ids(trace)
        self.assertEqual(evidence, {"doc-a"})
        self.assertEqual(MODULE.citation_grounding_quality([{"doc_id": "doc-a"}, {"doc_id": "doc-b"}], evidence), (1, 2, True))

    def test_citation_grounding_is_na_without_independent_evidence(self) -> None:
        grounded, total, verifiable = MODULE.citation_grounding_quality([{"doc_id": "doc-a"}], set())
        self.assertEqual((grounded, total), (0, 0))
        self.assertFalse(verifiable)

    def test_metrics_keep_citation_grounding_na_when_trace_evidence_missing(self) -> None:
        metrics = MODULE.build_metrics([{"actual_route": "chat", "expected_route": "chat", "passed": "Y", "bad_case": "", "citation_hit": "1/1"}])
        self.assertIsNone(metrics["citation_grounding_rate"])
        self.assertEqual(metrics["citation_grounding_total"], 0)

    def test_default_evaluation_input_is_frozen_enterprise_matrix(self) -> None:
        self.assertEqual(MODULE.DEFAULT_INPUT, "docs/整理与提升/enterprise_agent_eval_cases_130.csv")

    def test_rate_limit_error_preserves_retry_after_hint(self) -> None:
        error = MODULE.RateLimitError("limited", 60.0)
        self.assertEqual(error.retry_after, 60.0)

    def test_metrics_separate_infrastructure_failures_from_agent_quality(self) -> None:
        metrics = MODULE.build_metrics([
            {"actual_route": "chat", "expected_route": "chat", "passed": "Y", "bad_case": "", "latency_ms": "100", "tool_hit": "1/1", "keyword_hit": "1/1"},
            {"actual_route": "error", "expected_route": "chat", "passed": "N", "bad_case": "rate_limited", "latency_ms": "50", "tool_hit": "", "keyword_hit": ""},
            {"actual_route": "ops", "expected_route": "ops", "passed": "N", "bad_case": "tool_missing", "latency_ms": "200", "tool_hit": "0/1", "keyword_hit": "0/1"},
        ])
        self.assertEqual(metrics["overall_pass_rate"], 1 / 3)
        self.assertEqual(metrics["infrastructure_failure_cases"], 1)
        self.assertEqual(metrics["business_cases"], 2)
        self.assertEqual(metrics["business_passed_cases"], 1)
        self.assertEqual(metrics["business_pass_rate"], 0.5)
        self.assertEqual(metrics["business_route_accuracy"], 1.0)
        self.assertEqual(metrics["business_tool_success_rate"], 0.5)
        self.assertEqual(metrics["business_keyword_hit_rate"], 0.5)

    def test_classifies_business_model_timeout_as_timeout(self) -> None:
        self.assertEqual(
            MODULE.classify_failure(False, False, False, False, 'HTTP 504 http://local:8090/chat: {"code":50401,"message":"模型服务响应超时"}'),
            "timeout",
        )

    def test_classifies_rate_limit_before_timeout(self) -> None:
        self.assertEqual(MODULE.classify_failure(False, False, False, False, "HTTP 429 请求过于频繁"), "rate_limited")

    def test_classifies_completion_rejection_as_agent_quality_failure(self) -> None:
        self.assertEqual(
            MODULE.classify_failure(False, False, False, False, "API error /chat: {'code': 40011, 'message': 'chat task incomplete: tool_missing'}"),
            "completion_rejected",
        )
        metrics = MODULE.build_metrics([{"actual_route": "error", "expected_route": "chat", "passed": "N", "bad_case": "completion_rejected"}])
        self.assertEqual(metrics["infrastructure_failure_cases"], 0)
        self.assertEqual(metrics["business_cases"], 1)

    def test_classifies_model_entitlement_as_infrastructure_failure(self) -> None:
        self.assertEqual(
            MODULE.classify_failure(False, False, False, False, "HTTP 500 /chat: LLM HTTP 402 request_id=redacted"),
            "provider_entitlement",
        )
        metrics = MODULE.build_metrics([{"actual_route": "error", "expected_route": "chat", "passed": "N", "bad_case": "provider_entitlement"}])
        self.assertEqual(metrics["infrastructure_failure_cases"], 1)

    def test_evidence_source_requires_all_pipe_delimited_sources(self) -> None:
        output = '{"source": "prometheus"} {"source": "logs"}'
        self.assertTrue(MODULE.check_evidence_source("prometheus|logs", output))
        self.assertFalse(MODULE.check_evidence_source("prometheus|deployment_api", output))
    def test_tenant_binding_rejects_header_only_cross_tenant_claim(self) -> None:
        client = object.__new__(MODULE.EvalClient)
        client.current_user = lambda: {"tenant_id": "default"}

        with self.assertRaisesRegex(RuntimeError, "requested tenant does not match authenticated test identity"):
            MODULE.validate_tenant_binding(client, ["default", "knowledge-eval"])

    def test_tenant_binding_accepts_authenticated_tenant(self) -> None:
        client = object.__new__(MODULE.EvalClient)
        client.current_user = lambda: {"tenant_id": "test-tenant"}

        self.assertEqual(MODULE.validate_tenant_binding(client, ["test-tenant", "test-tenant"]), "test-tenant")

    def test_knowledge_workflow_evidence_accepts_verified_citation(self) -> None:
        self.assertTrue(MODULE.has_knowledge_workflow_evidence("正在查询文档", [{"snippet": "上传索引流程：调用 query_internal_docs"}]))
        self.assertFalse(MODULE.has_knowledge_workflow_evidence("正在查询文档", [{"snippet": "无关内容"}]))

    def test_citation_quality_requires_uploaded_document_version_in_workflow_contract(self) -> None:
        citations = [{"doc_id": "uploaded", "version": "generation-1", "chunk_id": "c", "source": "s", "snippet": "x"}]
        self.assertTrue(any(c.get("doc_id") == "uploaded" and c.get("version") for c in citations))

    def test_call_chat_deletes_exact_session_after_success(self) -> None:
        client = object.__new__(MODULE.EvalClient)
        client.tenant_id = "eval-tenant"
        calls = []
        client.create_session = lambda case_id, tenant_id=None: "sess-eval"
        client.post = lambda path, payload, tenant_id=None: calls.append((path, payload, tenant_id)) or {"trace_id": "trace-eval"}
        client.delete_session = lambda session_id, tenant_id: calls.append(("delete", session_id, tenant_id))

        response = client.call_chat("CASE-1", "safe question")

        self.assertEqual(response, {"trace_id": "trace-eval"})
        self.assertEqual(calls, [
            ("/chat", {"session_id": "sess-eval", "question": "safe question", "options": {"enable_rag": True, "enable_tools": True}}, "eval-tenant"),
            ("delete", "sess-eval", "eval-tenant"),
        ])

    def test_call_chat_deletes_exact_session_after_failure(self) -> None:
        client = object.__new__(MODULE.EvalClient)
        client.tenant_id = "eval-tenant"
        calls = []
        client.create_session = lambda case_id, tenant_id=None: "sess-eval"
        def fail_post(path, payload, tenant_id=None):
            raise RuntimeError("upstream failure")
        client.post = fail_post
        client.delete_session = lambda session_id, tenant_id: calls.append((session_id, tenant_id))

        with self.assertRaisesRegex(RuntimeError, "upstream failure"):
            client.call_chat("CASE-1", "safe question")
        self.assertEqual(calls, [("sess-eval", "eval-tenant")])

    def test_knowledge_flow_deletes_uploaded_document_after_index_failure(self) -> None:
        client = object.__new__(MODULE.EvalClient)
        client.knowledge_tenant_id = "knowledge-eval"
        calls = []
        client.upload_knowledge = lambda filename, content: {"doc_id": "doc-eval", "task_id": "task-eval"}
        client.get_index_task = lambda task_id: {"status": "failed", "chunk_count": 0}
        client.delete_knowledge = lambda doc_id: calls.append(doc_id) or {"status": "deleted"}

        with self.assertRaisesRegex(RuntimeError, "knowledge index failed"):
            MODULE.run_knowledge_flow(client, "RAG-FAIL", 1)
        self.assertEqual(calls, ["doc-eval"])
