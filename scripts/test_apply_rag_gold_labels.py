import importlib.util
import unittest


SPEC = importlib.util.spec_from_file_location("apply_rag_gold_labels", "scripts/apply_rag_gold_labels.py")
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ApplyRAGGoldLabelsTests(unittest.TestCase):
    def dataset(self):
        return [
            {"case_id": "RAG-001", "relevant_doc_ids": "", "relevance_label_source": "", "relevance_label_status": "", "input": "q"},
            {"case_id": "CHAT-001", "relevant_doc_ids": "", "relevance_label_source": "", "relevance_label_status": "", "input": "chat"},
        ]

    def gold(self):
        return [{"case_id": "RAG-001", "relevant_doc_ids": "doc-a", "relevance_label_source": "expert_review", "relevance_label_status": "verified"}]

    def test_merges_only_verified_gold_and_preserves_other_cases(self):
        merged, summary = MODULE.merge(self.dataset(), self.gold())
        self.assertEqual(merged[0]["relevant_doc_ids"], "doc-a")
        self.assertEqual(merged[0]["relevance_label_status"], "verified")
        self.assertEqual(merged[1]["case_id"], "CHAT-001")
        self.assertEqual(summary["applied_cases"], 1)
        self.assertEqual(summary["verified_cases_after_merge"], 1)

    def test_rejects_unknown_case_duplicate_gold_and_conflict(self):
        with self.assertRaises(ValueError):
            MODULE.merge(self.dataset(), [dict(self.gold()[0], case_id="missing")])
        with self.assertRaises(ValueError):
            MODULE.merge(self.dataset(), self.gold() + self.gold())
        conflicting = [dict(self.dataset()[0], relevant_doc_ids="doc-old", relevance_label_status="verified", relevance_label_source="human"), self.dataset()[1]]
        with self.assertRaises(ValueError):
            MODULE.merge(conflicting, self.gold())

    def test_rejects_unverified_or_unsupported_gold(self):
        with self.assertRaises(ValueError):
            MODULE.merge(self.dataset(), [dict(self.gold()[0], relevance_label_status="draft")])
        with self.assertRaises(ValueError):
            MODULE.merge(self.dataset(), [dict(self.gold()[0], relevance_label_source="model_seed")])

    def test_matching_existing_label_is_idempotent(self):
        existing = self.dataset()
        existing[0].update(self.gold()[0])
        merged, summary = MODULE.merge(existing, self.gold())
        self.assertEqual(merged[0]["relevant_doc_ids"], "doc-a")
        self.assertEqual(summary["already_matching_cases"], 1)


if __name__ == "__main__":
    unittest.main()
