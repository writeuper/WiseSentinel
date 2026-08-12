import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location("trace_access", Path(__file__).with_name("run_trace_access_contract.py"))
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class TraceAccessContractTests(unittest.TestCase):
    def test_contract_does_not_publish_trace_or_prompt_fields(self) -> None:
        source = Path(MODULE.__file__).read_text(encoding="utf-8")
        self.assertNotIn('"trace_id": trace_id', source)
        self.assertNotIn('"question":', source.split('print(', 1)[1])


if __name__ == "__main__":
    unittest.main()
