import unittest
from pathlib import Path


CHAT_SOURCE = Path(__file__).parents[1] / "portal" / "src" / "pages" / "Chat.tsx"


class PortalChatContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = CHAT_SOURCE.read_text(encoding="utf-8")

    def test_canceled_request_cannot_commit_stale_callbacks(self):
        self.assertIn("const requestEpochRef = useRef(0)", self.source)
        self.assertGreaterEqual(self.source.count("requestEpoch !== requestEpochRef.current"), 8)
        self.assertIn("requestEpochRef.current += 1", self.source)

    def test_session_creation_is_bound_to_request_epoch(self):
        self.assertIn("requestEpoch?: number", self.source)
        self.assertIn("requestEpoch !== undefined && requestEpoch !== requestEpochRef.current", self.source)
        self.assertIn("ensureSession(text.slice(0, 30), requestEpoch)", self.source)

    def test_stream_tool_and_citation_updates_are_immutable(self):
        self.assertIn("const citations = [...(last.citations || []), citation]", self.source)
        self.assertIn("const toolCalls = [...(last.toolCalls || []), { tool: data", self.source)
        self.assertNotIn("const citations = last.citations || [];\n              citations.push", self.source)
        self.assertNotIn("const toolCalls = last.toolCalls || [];\n              toolCalls.push", self.source)


if __name__ == "__main__":
    unittest.main()
