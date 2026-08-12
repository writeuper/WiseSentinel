import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("run_agent_eval.py")
SPEC = importlib.util.spec_from_file_location("agent_eval", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class EvalSessionCleanupTests(unittest.TestCase):
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
