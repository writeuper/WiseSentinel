# 阶段 50：L2 审批假成功消除与 Durable Intent 设计

日期：2026-08-11

## 缺陷与影响

跨架构、后端测试与前端审计确认：通用 L2 工具原先会创建 `tool_invoke` 审批并返回 `awaiting_approval`，但审批表只保存工具名和受抑制输入；批准后没有 intent、outbox、worker、幂等键或 Ops checkpoint 来恢复执行。

因此审批 UI 可以显示“已批准”，而原操作永不执行；Ops/Eino 包装器还可能将待审批当普通工具输出继续，形成“任务成功”的错误证据链。SRE 管理员此前还能直接绕过审批执行 L2。当前配置尚无 L2 adapter，该问题在首个生产写工具接入时会触发。

## 本阶段修复：fail-closed

在没有 immutable intent、受保护参数存储与 idempotent executor 前，不安全地“补一个 worker”不可接受。本阶段删除通用 Gateway 内的伪审批创建与管理员直执行旁路：

- 所有 L2 工具调用统一返回稳定 `50303`（高风险工具执行工作流尚未启用）。
- 不创建不可恢复的 `tool_invoke` approval，也不调用 adapter，避免产生已审批/已执行假象。
- 已有 Vector GC 专项 durable redrive 审批不受影响。

这不是将能力降级为普通拒绝，而是明确的上线安全门禁：只有完整 durable workflow 可用后才能注册 L2 写工具。

## 企业级主流落地方案（下一阶段设计）

引入 `ws_tool_execution_intent` 和 transactional outbox，完整参数只进入带 KMS envelope encryption、目的绑定、RBAC、访问审计和留存期的 command vault；Trace、approval、OpsTask、ToolCall、日志和 Portal 只保留投影。

```text
awaiting_approval --审批 CAS + SoD--> runnable --DB claim/token--> running
       | rejected/expired/reauth_required                       | fenced finish
       +--------------------------------------------------------> succeeded/failed
```

关键不变量：

- approval、intent、父 Ops task 和参数 hash/策略版本/目标版本必须事务绑定；批准时和执行前重新授权。
- 发起人不能审批自己的 L2 intent；过期、跨租户、策略变化、工具禁用或 hash 不一致 fail-closed。
- outbox 可至少一次投递，但 MySQL CAS、execution token 和下游 `(tenant_id,intent_id)` 幂等键共同防重放。
- 命中 L2 的 Ops 图停在 `awaiting_approval`，禁止重跑非确定性 LLM 图；首切片仅执行被冻结的一次工具调用。

## 回归证据

| 验证 | 结果 |
| --- | --- |
| L2 Gateway 单测：operator / sre_admin / platform_admin | 均返回 50303 |
| L2 fake adapter 副作用计数 | 0 次 |
| 全量 `go test ./...` | 通过 |
| 后端构建与敏感 sink 静态门禁 | 通过 |
| 新构建 `/health/ready` | 通过 |

## 后续前置条件

在启用任何 L2 写工具前，必须先确定每类工具的 target-level policy、审批窗口、SoD/quorum、外部幂等协议、授权再校验和加密 command vault。届时按上述状态机实现 MySQL/Redis 集成测试：并发 approve/reject/expire、双 worker/旧 token、崩溃恢复、跨租户、self-approval、canary 无泄露及“恰好一次业务效果”验证。
