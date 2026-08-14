import importlib.util
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("validate_rag_judgments.py")
SPEC = importlib.util.spec_from_file_location("validate_rag_judgments", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ValidateRAGJudgmentsTests(unittest.TestCase):
    def rows(self):
        return [
            {"case_id": "RAG-001", "annotator_id": "a", "doc_id": "doc-a", "relevance": "relevant", "review_status": "adjudicated"},
            {"case_id": "RAG-001", "annotator_id": "a", "doc_id": "doc-b", "relevance": "not_relevant", "review_status": "adjudicated"},
            {"case_id": "RAG-001", "annotator_id": "b", "doc_id": "doc-a", "relevance": "relevant", "review_status": "adjudicated"},
            {"case_id": "RAG-001", "annotator_id": "b", "doc_id": "doc-b", "relevance": "not_relevant", "review_status": "adjudicated"},
            {"case_id": "RAG-002", "annotator_id": "a", "doc_id": "doc-a", "relevance": "relevant", "review_status": "draft"},
            {"case_id": "RAG-002", "annotator_id": "b", "doc_id": "doc-a", "relevance": "not_relevant", "review_status": "draft"},
        ]

    def test_double_review_agreement_and_gold_output(self):
        report, gold = MODULE.run(self.rows(), minimum_verified=1)
        self.assertEqual(report["case_count"], 2)
        self.assertEqual(report["eligible_verified_cases"], 1)
        self.assertEqual(report["agreement_cases"], 1)
        self.assertEqual(report["exact_agreement_rate"], 0.5)
        self.assertEqual(gold, [{"case_id": "RAG-001", "relevant_doc_ids": "doc-a", "relevance_label_source": "expert_review", "relevance_label_status": "verified"}])

    def test_disagreement_is_not_promoted_to_verified_gold(self):
        rows = [row for row in self.rows() if row["case_id"] == "RAG-002"]
        for row in rows:
            row["review_status"] = "adjudicated"
        report, gold = MODULE.run(rows, minimum_verified=1)
        self.assertEqual(report["eligible_verified_cases"], 0)
        self.assertEqual(gold, [])
        self.assertFalse(report["strict_gate_passed"])

    def test_duplicate_and_invalid_rows_fail_without_gold(self):
        rows = self.rows()[:1] + [dict(self.rows()[0], relevance="unknown")]
        report, gold = MODULE.run(rows, minimum_verified=1)
        self.assertTrue(report["validation_errors"])
        self.assertEqual(gold, [])

    def test_strict_sample_gate_is_explicit(self):
        report, _ = MODULE.run(self.rows(), minimum_verified=100)
        self.assertFalse(report["strict_gate_passed"])
        self.assertEqual(report["minimum_verified_required"], 100)

    def test_summary_does_not_contain_document_ids(self):
        report, _ = MODULE.run(self.rows(), minimum_verified=1)
        self.assertNotIn("doc-a", str(report))


if __name__ == "__main__":
    unittest.main()
