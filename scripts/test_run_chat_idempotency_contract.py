import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location("chat_idempotency", Path(__file__).with_name("run_chat_idempotency_contract.py"))
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ChatIdempotencyContractTests(unittest.TestCase):
    def test_global_model_metrics_are_not_an_idempotency_oracle(self) -> None:
        self.assertFalse(hasattr(MODULE.Client, "model_calls"))

    def test_conflict_code_is_stable(self) -> None:
        self.assertEqual(MODULE.CONFLICT_CODE, 40901)
