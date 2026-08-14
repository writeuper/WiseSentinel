import importlib.util
import unittest


SPEC = importlib.util.spec_from_file_location("build_rag_judgment_template", "scripts/build_rag_judgment_template.py")
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class BuildRAGJudgmentTemplateTests(unittest.TestCase):
    def test_emits_unlabeled_rows_for_two_reviewers_and_deduplicates_candidates(self):
        cases = [{
            "case_id": "RAG-001", "scene": "Chat-RAG-Runbook",
            "expected_tools": "query_internal_docs", "expected_knowledge": "Runbook",
            "retrieved_doc_ids": "doc-a|doc-a|doc-b|doc-c",
        }]
        rows, summary = MODULE.build_template_rows(cases, ["reviewer-a", "reviewer-b"], top_k=2)
        self.assertEqual(len(rows), 4)
        self.assertEqual(summary["candidate_rows"], 4)
        self.assertEqual(summary["labeled_rows"], 0)
        self.assertEqual({row["relevance"] for row in rows}, {""})
        self.assertEqual({row["review_status"] for row in rows}, {"draft"})
        self.assertEqual({row["candidate_rank"] for row in rows}, {"1", "2"})

    def test_non_rag_and_missing_retrieval_cases_are_reported(self):
        cases = [
            {"case_id": "CHAT-001", "scene": "Chat-平台问答", "expected_tools": "", "retrieved_doc_ids": "doc-a"},
            {"case_id": "RAG-001", "scene": "Chat-RAG-Runbook", "expected_tools": "query_internal_docs", "retrieved_doc_ids": ""},
        ]
        rows, summary = MODULE.build_template_rows(cases, ["a", "b"])
        self.assertEqual(rows, [])
        self.assertEqual(summary["rag_cases"], 1)
        self.assertEqual(summary["cases_missing_retrieval"], 1)

    def test_requires_two_distinct_reviewers_and_positive_top_k(self):
        with self.assertRaises(ValueError):
            MODULE.build_template_rows([], ["same", "same"])
        with self.assertRaises(ValueError):
            MODULE.build_template_rows([], ["a", "b"], top_k=0)


if __name__ == "__main__":
    unittest.main()
