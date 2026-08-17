# 阶段一：Agent Runtime 任务完成闭环设计与验证

## 1. 范围与验收口径

本次实现将 Ops Agent 的“任务结束”从模型输出事件改为平台校验后的状态转换；Chat 在启用工具时也在模型调用前建立轻量契约并写入 `task_contract` Trace Step，通过内部 `task_complete` 工具提交完成提议。两条路径都由平台证据校验，不以模型文本作为成功依据。

成功不再表示模型给出了文本，而必须同时满足：

1. 任务开始时已固化 `TaskContract`；
2. 最终输出有摘要且检查项全部完成；
3. 至少有一个平台记录的成功工具调用；
4. 成功工具在契约的允许工具列表中；
5. 没有突破 `RiskBudget`。
6. 不存在未处理的工具失败或超时步骤。

不满足时任务终态为 `incomplete`，原因是稳定枚举 `tool_missing`、`task_incomplete`、`budget_exceeded` 或 `scope_violation`，不会伪装成 `success`。

## 2. 设计

```text
OpsAnalyze
  -> 固化 TaskContract (工具/预算/配置/主体/Trace)
  -> ws_ops_task.task_contract_json
  -> Plan-Execute-Replan / Focused Tool
  -> TaskCompletion + 平台 Evidence
  -> ValidateTaskCompletion
       -> accepted       -> success
       -> rejected       -> incomplete + completion_json + Trace step

Chat (EnableTools)
  -> TaskContract (Trace scope)
  -> ToolGateway Evidence / Citation
  -> task_complete
  -> ValidateTaskCompletion -> response or 422 incomplete
```

### 数据模型

`internal/domain/task_contract.go` 新增：

- `TaskContract`：任务、租户、用户、目标、工具/资源范围、禁止动作、完成条件、预算、配置版本和 Trace；
- `TaskCompletion`：完成摘要与检查项；
- `CompletionDecision`：平台验证结论和缺失项。

`ws_ops_task` 增加 `task_contract_json`、`completion_json`，迁移文件为 `manifest/sql/migrations/20260816_task_completion_contract.sql`。迁移采用 information_schema 条件检查，可重复执行；新字段可空，旧任务可继续读取。

### 安全不变量

- 不信任模型的“已完成”自然语言，只信任 ToolGateway 写入的 `Evidence`；
- 一次执行仅能使用提交时锁定的工具集合；
- `completion_json` 的写入使用 execution token 围栏，过期 worker 不能覆盖当前尝试；
- 任务投影和完成记录都经既有脱敏投影后持久化。

### 指标与证据

新增 Prometheus 指标 `ws_task_completion_checks_total{outcome}`，标签限定为 `accepted`、`tool_missing`、`task_incomplete`、`budget_exceeded`、`scope_violation`。Ops 任务时延指标同步接受 `incomplete`、`cancelled` 状态。

## 3. 核心代码阅读路线

1. `internal/agent/ops/agent.go` 的 `Analyze`：提交前从 Runtime Config 或 ToolGateway 生成工具白名单，序列化后与任务同写入。
2. 同文件 `executeTask`：运行结束后先反序列化持久化契约，再调用 `ValidateTaskCompletion`；验证失败写入 `incomplete`，不会创建故障知识草稿。
3. `internal/domain/task_contract.go`：纯函数验证器，适合用表驱动单测覆盖边界，不需要数据库或模型。
4. `internal/repository/ops_task.go`：`SetCompletionIfOwned` 与最终状态写入都依赖 `(tenant_id, task_id, status=running, execution_token)`，保持已有租约围栏语义。
5. `internal/agent/chat/task_complete.go`：模型只能提交结构化完成提议；该工具不执行外部动作，最终接受与否仍由运行时决定。
6. `internal/agent/ops/task_complete.go` 与 `executor.go`：同一控制面协议被加入 Plan-Execute-Replan 的原生 Eino 工具集合；非 Focused Tool 路径缺少显式提交会进入 `incomplete`，不再由最终文本隐式成功。

## 4. 问题定位与修复记录

### P1：模型输出可绕过完成条件

**现象**：原 `executeTask` 只要 `runAgent` 不报错，就无条件以 `success` 持久化结果；无工具调用的最终文本和兜底文本也会被标记完成。

**修复**：在成功持久化前引入契约校验；未发现成功证据时落入 `incomplete/tool_missing`。

### P2：HTTP 请求的 MaxIterations 未被执行器采用

**现象**：控制器传入 `req.MaxIterations`，但 `runAgent` 仅参考运行时配置和内部默认值。

**修复**：抽取 `effectiveMaxIterations`，优先采用更严格的请求/配置值，并将其固化为 `RiskBudget`。

### P3：取消状态没有取消实际执行

**现象**：原先仅有 Trace/HTTP 上下文取消，异步 Ops Worker 使用独立运行上下文，用户无法终止已领取任务。

**修复**：新增 `POST /ops/tasks/{task_id}/cancel`。仓储转换清空 execution token 以围栏过期 worker；运行时以短周期读取任务所有权，发现 `cancelled` 或 token 变化即取消模型/工具上下文。终态任务拒绝取消，重复取消幂等。

### P4：新增 JSON 列破坏旧任务提交

**现象**：真实 MySQL 集成测试发现旧调用方未提供任务契约时，仓储向 JSON 列写入空字符串；MySQL 8 以 `Error 3140: Invalid JSON text` 拒绝写入。

**修复**：仅在非空时写入 `task_contract_json` / `completion_json`，使 nullable JSON 列保留 `NULL`，并新增集成回归覆盖旧创建、契约读回、取消及迟到 owner 写入拒绝。

### P5：取消后 Trace 可能表现为失败或保持运行

**现象**：Ops Worker 的取消会中断执行 context；若直接用该 context 收尾，Trace 更新可能被数据库驱动拒绝，并把预期的 `cancelled` 误归类为 `failed`。

**修复**：在 Trace 收尾时使用 `context.WithoutCancel` 保留 tenant/trace 身份；当执行错误是 `context.Canceled` 且任务投影已为 `cancelled`，返回取消终态而非重试/失败。

## 5. 测试矩阵与执行证据

| 风险场景 | 自动化断言 |
|---|---|
| 有成功、范围内工具证据 | 允许完成 |
| 模型直接给结论 | `tool_missing` |
| 检查项漏做 | `task_incomplete` |
| 超过预算 | `budget_exceeded` |
| 成功调用越出工具范围 | `scope_violation` |
| 契约/完成信息持久化 | 仓储目标字段和 execution token 围栏 |
| TaskComplete 非法状态 | 工具拒绝，不写完成提议 |
| Ops 普通最终文本未调用 TaskComplete | `task_incomplete`，不写 `success` |
| 取消 pending/running 任务 | `cancelled` 终态、token 围栏和运行上下文取消 |

已执行的快速回归：

```bash
go test ./internal/domain ./internal/agent/ops ./internal/observability ./internal/repository ./internal/gateway/metrics
```

已执行完整仓库回归与补丁空白检查：

```bash
go test ./...
git diff --check
```

结果：全部通过；数据库迁移及仓储 CAS 行为的真实 MySQL 验证见下节。

实际 MySQL（Docker `wisesentinel-mysql`）验证已补充执行：

```bash
docker exec -i wisesentinel-mysql mysql -uws -pws123 wisesentinel < manifest/sql/migrations/20260816_task_completion_contract.sql
OPS_TEST_MYSQL_DSN='mysql:ws:ws123@tcp(127.0.0.1:3306)/wisesentinel?charset=utf8mb4&parseTime=True&loc=Local' \
  go test -tags integration ./internal/repository \
  -run 'TestOpsTask(LeaseCASAndBoundedRetry|ContractAndCancellationFence)Integration' -v
```

结果：迁移后两个 JSON 字段均存在；旧任务不提供契约时保存 `NULL` 而非非法空 JSON；契约读回、取消、execution token 清空和迟到 owner 完成写入拒绝均通过。

## 6. 深度审计结论（2026-08-17）

| 审计点 | 证据 | 结论 |
|---|---|---|
| 契约创建时机 | Chat 在模型调用前写 `task_contract` Trace Step；Ops 在落库前序列化契约 | 通过 |
| 模型自述不能替代证据 | `ValidateTaskCompletion` 只读取 ToolGateway Evidence / Citation；`evidence_ids` 不作为判定输入 | 通过 |
| 显式完成 | Chat、Ops 的 Eino 工具集均有 `task_complete`；非 Focused Ops 文本缺少提议即为 `incomplete` | 通过 |
| 取消一致性 | MySQL 集成测试验证 token 围栏；取消 context 的 Trace 用 `WithoutCancel` 收尾 | 通过 |
| 静态与回归门禁 | `go vet ./...`、`go test ./...`、`git diff --check` | 通过 |

仍需明确的阶段二/三边界：

1. `AllowedResources`、`ForbiddenActions` 已进入契约数据模型，但现有工具 Evidence 只有工具名和摘要，没有结构化资源标识；因此尚不能做资源级范围或禁止动作的强制判定。
2. 本地没有配置可用于安全测试的 LLM provider 凭据；TaskComplete 的工具 schema、sink、拒绝路径和运行时校验已自动化验证，但真实 provider 是否稳定遵循 function-calling 选择仍需在隔离测试租户执行端到端 Case。
3. Chat 与 Ops 暂各有一个同协议的内部 TaskComplete 实现。后续应提取为 `internal/runtime` 共享控制面包，以防 schema 演进漂移。

## 7. 后续迭代边界

- 将 Planner/Replanner 的完成声明映射到相同验证器，并评估在 planexecute 的 Replanner Respond 终态强制 TaskComplete 的框架扩展方式；
- 评估是否需要为 Chat 契约增加独立持久化表（当前以 Trace 为审计证据，避免为同步问答引入不必要的任务存储）；
- 增加 Trace 事件序号和原始重放快照（阶段三范围）。
