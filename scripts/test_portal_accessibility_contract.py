import unittest
from pathlib import Path


PORTAL = Path(__file__).parents[1] / "portal" / "src" / "pages"


class PortalAccessibilityContractTests(unittest.TestCase):
    def read(self, name: str) -> str:
        return (PORTAL / name).read_text(encoding="utf-8")

    def test_chat_exposes_named_input_controls_and_recovery(self):
        source = self.read("Chat.tsx")
        for marker in (
            'aria-label="Agent 问题输入"',
            'aria-label="启用 RAG 知识库"',
            'aria-label="启用工具调用"',
            'aria-label="启用流式响应"',
            'aria-label="创建新对话"',
            'aria-label={streaming ? \'生成中\' : sending ? \'发送中\' : \'发送问题\'}',
            'aria-label="重试发送上一条消息"',
        ):
            self.assertIn(marker, source)

    def test_ops_and_approval_controls_have_operational_names(self):
        ops = self.read("Ops.tsx")
        approvals = self.read("Approvals.tsx")
        for marker in (
            'aria-label="Ops 告警分析提示词"',
            'aria-label="异步模式"',
            'aria-label="最大 Agent 执行步数"',
            'aria-label="开始告警分析"',
            'aria-label="按任务状态筛选"',
            'aria-label="刷新 Ops 任务列表"',
        ):
            self.assertIn(marker, ops)
        for marker in (
            'aria-label={`批准审批 ${row.approval_id}`}',
            'aria-label={`拒绝审批 ${row.approval_id}`}',
            'aria-label="刷新审批列表"',
            'aria-label="审批意见"',
        ):
            self.assertIn(marker, approvals)

    def test_trace_failure_has_named_retry_action(self):
        self.assertIn('aria-label="重新加载 Trace"', self.read("Trace.tsx"))


if __name__ == "__main__":
    unittest.main()
