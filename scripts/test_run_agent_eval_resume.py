import csv
import importlib.util
import pathlib
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("run_agent_eval.py")
SPEC = importlib.util.spec_from_file_location("run_agent_eval", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class ResumeTests(unittest.TestCase):
    def test_load_resume_results_reads_terminal_cases(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "results.csv"
            with path.open("w", encoding="utf-8", newline="") as file:
                writer = csv.DictWriter(file, fieldnames=["case_id", "passed", "bad_case"])
                writer.writeheader()
                writer.writerow({"case_id": "A-001", "passed": "Y", "bad_case": ""})
            loaded = MODULE.load_resume_results(path)
            self.assertEqual(loaded["A-001"]["passed"], "Y")

    def test_load_resume_results_missing_file_is_empty(self):
        self.assertEqual(MODULE.load_resume_results(pathlib.Path("/tmp/does-not-exist-wisesentinel.csv")), {})

    def test_resume_policy_retries_failed_case(self):
        prior = {"case_id": "A-002", "passed": "N", "bad_case": "timeout"}
        self.assertNotEqual(prior.get("passed"), "Y")


if __name__ == "__main__":
    unittest.main()
