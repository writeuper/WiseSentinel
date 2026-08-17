# CHAT-006：TaskComplete 补交回合修复记录

## 问题

`CHAT-006`（“如何理解任务完成率和工具成功率”）在首轮 GLM 冒烟中出现：RAG 检索和模型生成均成功，但模型直接结束文本回答，没有提交 `task_complete`，平台按阶段一任务合同返回 `422 incomplete`。

该行为不能通过平台本地伪造 `status=completed` 来修复，否则会破坏“平台只信任模型显式完成提议与真实 Evidence”的不变量。

## 修复

新增 `internal/agent/chat/completion_repair.go`：

1. 主 ReAct 结束时，如果没有完成提议、但已有成功且在 allowlist 内的工具/Citation Evidence，则启动一次补交回合；
2. 补交回合只向 LLM 暴露 `task_complete` 一个工具，并以 `tool_choice=required` 强制 Function Calling；
3. LLM 必须输出 `status=completed`、非空 `summary`、所有项完成的 `checklist`；
4. 平台使用既有 `taskCompleteTool` 解析该 LLM tool call，再使用既有 `ValidateTaskCompletion` 审核真实 Evidence；
5. LLM 不调用、参数非法、没有证据、工具失败或校验失败时，任务仍为 `incomplete`，不会被兜底标记成功。

同步 Chat 与 SSE 流式路径都应用该逻辑，并在 Trace 中保留 `completion_repair/force_task_complete` 的成功或错误事件（仅在实际触发补交时出现）。

## 验证

自动化验证：

```bash
go test ./internal/agent/chat ./internal/domain
python3 scripts/test_run_agent_eval.py
git diff --check
```

新增单元测试验证补交模型：

- 仅绑定 `task_complete`；
- 强制 `tool_choice=forced`（适配层映射为 OpenAI-compatible `required`）；
- 只接受 LLM tool call 中的 `status=completed`；
- 有失败或越界 Evidence 时不允许触发补交。

真实 GLM 复测：

```text
CHAT-006: HTTP 200 / code 0 / passed
Trace: rag.retrieve=success → model.generate=success → completion.task_complete=success
```

本次真实复测中，更新后的主 Prompt 已让 GLM 主动提交 `task_complete`，因此补交回合未触发；这是正常路径。补交逻辑由确定性单元测试覆盖，作为模型偶发漏调时的受控保护层。
