import importlib.util
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("report_eval_dataset_coverage.py")
SPEC = importlib.util.spec_from_file_location("eval_coverage", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class EvalDatasetCoverageTests(unittest.TestCase):
    def test_coverage_counts_routes_tools_negative_cases_and_verified_labels(self):
        report = MODULE.analyze_cases([
            {"case_id": "A", "scene": "chat", "input": "q", "expected_route": "chat", "expected_tools": "get_current_time", "forbidden_tools": "search_logs", "expected_keywords": "time", "relevant_doc_ids": "", "relevance_label_status": "", "relevance_label_source": ""},
            {"case_id": "B", "scene": "rag", "input": "q", "expected_route": "chat", "expected_tools": "query_internal_docs", "forbidden_tools": "", "expected_keywords": "citation", "relevant_doc_ids": "doc-1", "relevance_label_status": "verified", "relevance_label_source": "expert_review"},
        ], minimum_cases=2)
        self.assertEqual(report["case_count_gate"], "passed")
        self.assertEqual(report["expected_tool_cases"], 2)
        self.assertEqual(report["forbidden_tool_cases"], 1)
        self.assertEqual(report["relevance_verified_cases"], 1)
        self.assertEqual(report["tool_coverage"]["query_internal_docs"], 1)

    def test_coverage_artifact_can_carry_provenance_fields(self):
        report = MODULE.analyze_cases([], minimum_cases=0)
        report["generated_at"] = "2026-08-14T00:00:00Z"
        report["dataset_sha256"] = "a" * 64
        self.assertEqual(len(report["dataset_sha256"]), 64)

    def test_coverage_fails_structural_and_size_gates(self):
        report = MODULE.analyze_cases([
            {"case_id": "A", "scene": "", "input": "", "expected_route": "", "expected_tools": "", "forbidden_tools": "", "expected_keywords": "", "relevant_doc_ids": "", "relevance_label_status": "", "relevance_label_source": ""},
            {"case_id": "A", "scene": "", "input": "q", "expected_route": "chat", "expected_tools": "", "forbidden_tools": "", "expected_keywords": "", "relevant_doc_ids": "", "relevance_label_status": "", "relevance_label_source": ""},
        ], minimum_cases=3)
        self.assertEqual(report["case_count_gate"], "failed")
        self.assertEqual(report["structural_gate"], "failed")
        self.assertEqual(report["duplicate_case_id_count"], 1)
        self.assertEqual(report["missing_input_cases"], 1)

    def test_csv_reader_handles_utf8_bom(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "cases.csv"
            path.write_text("\ufeffcase_id,scene,input,expected_route\nA,chat,hello,chat\n", encoding="utf-8")
            self.assertEqual(MODULE.read_cases(path)[0]["case_id"], "A")


if __name__ == "__main__":
    unittest.main()
