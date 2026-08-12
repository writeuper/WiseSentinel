import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location("sse_contract", Path(__file__).with_name("run_sse_chat_contract.py"))
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class SSEContractParsingTests(unittest.TestCase):
    def test_classifies_complete_stream(self) -> None:
        self.assertEqual(MODULE.stream_outcome([("connected", "{}"), ("message", "safe"), ("done", "{}")]), "success_done")

    def test_classifies_structured_overload_before_done_as_failure(self) -> None:
        events = [("error", '{"code":50304,"retry_after_seconds":2}'), ("done", "{}")]
        self.assertEqual(MODULE.stream_outcome(events), "sse_50304_retry_2")

    def test_does_not_classify_generic_error_as_success(self) -> None:
        self.assertEqual(MODULE.stream_outcome([("error", "stable error"), ("done", "{}")]), "sse_error")

    def test_delete_race_requires_missing_done_and_hidden_session(self) -> None:
        class FakeClient:
            def __init__(self) -> None:
                self.deleted = False

            def create_session(self, suffix: str) -> str:
                self.assert_suffix = suffix
                return "sess-test"

            def delete_session(self, session_id: str) -> None:
                if session_id != "sess-test":
                    raise AssertionError(session_id)
                self.deleted = True

            def read_stream(self, session_id: str, question: str, cancel_on_message: bool = False, on_event=None):
                if session_id != "sess-test" or cancel_on_message:
                    raise AssertionError("unexpected stream request")
                on_event("connected")
                return [("connected", "{}"), ("error", "safe")]

            def request(self, method: str, path: str):
                if (method, path) != ("GET", "/sessions/sess-test/messages"):
                    raise AssertionError("unexpected history request")
                raise RuntimeError("api_40401")

        fake = FakeClient()
        self.assertEqual(MODULE.run_delete_race(fake, "question"), {"outcome": "delete_race_not_completed"})
        self.assertTrue(fake.deleted)
