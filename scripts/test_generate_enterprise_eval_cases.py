import csv
import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("generate_enterprise_eval_cases.py")
SPEC = importlib.util.spec_from_file_location("enterprise_eval_generator", SCRIPT)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class EnterpriseEvalGeneratorTests(unittest.TestCase):
    def test_matrix_is_large_unique_and_valid(self):
        cases = MODULE.build_cases()
        MODULE.validate_cases(cases)
        self.assertGreaterEqual(len(cases), 120)
        self.assertEqual(len(cases), len({case["case_id"] for case in cases}))
        self.assertGreaterEqual(sum(case["expected_route"] == "chat" for case in cases), 30)
        self.assertGreaterEqual(sum(case["expected_route"] == "ops" for case in cases), 80)
        self.assertGreaterEqual(sum(bool(case["expected_knowledge"]) for case in cases), 20)
        self.assertGreaterEqual(sum("|" in case["expected_tools"] for case in cases), 15)
        self.assertGreaterEqual(sum(bool(case["forbidden_tools"]) for case in cases), 15)

    def test_csv_output_is_deterministic_and_schema_compatible(self):
        first, second = MODULE.build_cases(), MODULE.build_cases()
        self.assertEqual(first, second)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "cases.csv"
            MODULE.write_csv(output, first)
            with output.open(encoding="utf-8-sig", newline="") as handle:
                reader = csv.DictReader(handle)
                self.assertEqual(reader.fieldnames, MODULE.FIELDS)
                rows = list(reader)
            self.assertEqual(len(rows), len(first))
            self.assertEqual(rows[0]["case_id"], "CHAT-001")
            self.assertEqual(rows[-1]["case_id"], "NEG-015")


if __name__ == "__main__":
    unittest.main()
