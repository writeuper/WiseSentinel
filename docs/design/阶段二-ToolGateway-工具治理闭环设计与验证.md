# 阶段二：ToolGateway 工具治理闭环设计与验证

## 1. 实施范围

阶段二把工具治理约束集中在 `ToolGateway.Invoke` 唯一执行入口，覆盖工具存在性/启用状态、Agent allowlist、审批绑定、资源范围、输入 schema、查询时间范围、任务级预算、调用指纹和结构化错误。无 `TaskID` 的旧调用保持兼容，不启用跨请求去重。

## 2. 调用不变量

1. 模型或 Agent 不能绕过 Gateway 直接获得适配器。
2. 指纹为 `SHA-256(tenant_id, task_id, tool_name, canonical_json(input), resource_scope)`；对象键顺序不影响结果，租户和任务始终隔离。
3. 同一任务的相同指纹在短时间窗口内只允许一次执行；首版拒绝重复调用，不复用其他调用的原始输出，避免数据串用。
4. `ToolBudgetState` 随一次 Agent 执行上下文传播，限制总调用数、单工具调用数、总延迟、输出字节、查询时间跨度和重试次数。
5. 适配器错误对外统一为 `TOOL_TIMEOUT` 或 `TOOL_UPSTREAM_ERROR`，内部日志保留脱敏诊断。

## 3. 结构化错误映射

| 场景 | AppError | HTTP |
|---|---|---:|
| 工具不存在/无适配器 | `TOOL_NOT_FOUND` | 404 |
| 工具关闭 | `TOOL_DISABLED` | 403 |
| Agent 不在 allowlist | `AGENT_TOOL_DENIED` | 403 |
| 参数缺失/类型错误/枚举范围错误 | `ARGUMENT_MISSING` / `SCHEMA_INVALID` / `ARGUMENT_OUT_OF_RANGE` | 422 |
| 相同任务相同调用 | `TOOL_DUPLICATE` | 409 |
| 任务预算耗尽 | `TOOL_BUDGET_EXCEEDED` | 429 |
| 超时/上游失败 | `TOOL_TIMEOUT` / `TOOL_UPSTREAM_ERROR` | 504 / 502 |
| 缺少审批或资源范围不符 | `APPROVAL_REQUIRED` / `RESOURCE_DENIED` | 403 |

## 4. 验证证据

单元测试：

```bash
GOTMPDIR=/dev/shm go test -count=1 ./internal/toolkit ./internal/agent/chat ./internal/agent/ops ./internal/domain
```

已覆盖：规范化 JSON 指纹、任务隔离、重复调用拦截、任务预算、Agent allowlist；阶段一原有 Gateway、Chat、Ops 测试仍通过。

真实运行态阶段一证据：普通 Chat/SSE/Trace、工具证据链和异步 Ops 取消均已通过，Trace 链为 `task_contract.created → tool.get_current_time → completion.task_complete(success)`。

2026-08-25 部署后真实 API 验收：

- `POST /ops/analyze` 的 allowlist 内工具 `query_prometheus_alerts` 返回 `200`、`success`，并产生结构化 Evidence；
- 同一全局工具集中存在、但租户 Ops 契约未允许的 `search_logs` 返回 `403 / 40311 (AGENT_TOOL_DENIED)`；
- Trace 授权、SSE 基础流和 SSE 取消合约均返回预期成功结果。

## 5. 已知边界与后续

- 当前去重状态是单进程短窗口内存状态；多副本部署应迁移到 Redis/数据库唯一键，并增加清理任务。
- `ToolCallRecord` 目前保留脱敏输入输出但未持久化指纹，后续迁移增加 `task_id/fingerprint/resource_scope` 索引。
- “高相似但分页参数不同”的语义去重尚未默认拦截；首版只拒绝完全相同 canonical 输入，避免误伤合法分页。
- Agent 配置的工具 allowlist 已作为契约来源，Gateway 仍以全局工具元数据和 Agent allowlist 做最终 fail-closed 校验。
