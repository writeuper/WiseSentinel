# 阶段 99：Chat/RAG 长尾 Trace 语义治理
日期：2026-08-12

## 目标

针对阶段 98 冻结在线集的 Chat/RAG 长尾（P95 20.16s）建立可追溯、语义正确的分段观测，并确保用户可把 Chat 结果安全关联到 Trace。覆盖同步 Chat、SSE、RAG 成功/空结果/错误/跳过、Trace 脱敏与 Portal 展示。

## 角色协作

| 角色 | 发现与落地 |
| --- | --- |
| 自动化测试工程师 | 用真实 API 分别执行同步和 SSE RAG Chat，读取同租户 Trace 并断言阶段状态、耗时和空错误字段；执行后删除精确创建的会话。 |
| Agent 架构师 | 明确不变量：观测必须区分 `success`、`empty`、`skipped`、`error`。无命中不是基础设施失败，禁用 RAG 不是检索失败；否则 Recall、失败率、容量归因和运营决策都不可信。 |
| Golang 后端工程师 | 为同步与 SSE 持久化 RAG Trace step，新增明确状态；修复空错误字段被脱敏函数变为“suppressed bytes=0”从而伪装错误的语义污染。 |
| 前端工程师 | Chat 页面为新生成回答展示短 Trace ID；只显示关联句柄，不展示 prompt、文档正文、模型输出或工具负载。同步和 SSE done 都可写入该 ID。 |

## 问题、根因与企业级方案

### 1. RAG 阶段总被标记 success

**问题**：原同步 Chat 无论 RAG 检索报错、无命中或被禁用，都写 `rag/retrieve: success`；SSE 完全不写 Agent Trace。该行为会把“空召回”和“依赖故障”错误计入成功，也无法解释流式长尾。

**企业级方案**：

- 采用有界枚举的阶段结果（success/empty/skipped/error），而不是从 message 文本推断；指标/Trace 只含低基数状态。
- RAG 质量报表以 `success + empty` 为检索完成分母，依赖失败单列；Recall@K 只从带标注且可检索的样本计算。
- SSE 与同步请求共享同一 Trace 生命周期和脱敏策略；done 后才作为可完成的会话事实。

**实现**：`retrieveDocs` 返回安全 Prompt 投影、citations、状态和受控错误摘要；同步与 SSE 均记录 `rag/retrieve`。SSE 在 goroutine 内启动/结束 Trace，并把 Tool step sink 接入既有持久化记录。

### 2. 空 error_msg 被持久化成非空脱敏对象

**问题**：`TelemetryProjection("")` 产生 `{"suppressed":true,"bytes":0}`。成功 RAG step 的 `error_msg` 因此在 API 中非空，前端和评测系统会将其误认为存在错误。

**企业级方案**：可选诊断字段必须保留缺失语义；只对实际非空内容脱敏。不得为避免泄露而制造虚假错误，安全和真实性必须同时成立。

**实现**：Trace repository 新增 `optionalTelemetryProjection`，空 input/output/error 字段保持空；非空值仍使用 fail-closed telemetry projection，且 canary 测试验证不泄露。

## 真实验证

| 场景 | 结果 |
| --- | --- |
| 同步 RAG Chat | Trace success；`rag/retrieve=success`；RAG 816ms；`error_msg=null`；请求总耗时约 17.3s |
| SSE RAG Chat | 收到 citation/connected/message/done；Trace success；`rag/retrieve=success`；RAG 158ms；`error_msg=null` |
| 长尾归因 | 两个真实样本 RAG 仅 0.16–0.82s，而同步总耗时约 17.3s；当前长尾主因仍是模型/ReAct generation，不应归因于 Milvus 检索 |
| RAG 状态单测 | 覆盖 RAG 未装配/error/empty/success/请求关闭 RAG，状态分别为 error/error/empty/success/skipped |
| Portal | 生产构建通过；新增短 Trace ID 展示 |

## 回归门禁

- `go test ./... -count=1`：通过。
- `go test -race ./internal/agent/chat ./internal/repository ./internal/observability ./internal/gateway/metrics -count=1`：通过。
- 在线脚本单测 17 条、敏感 Sink 静态检查、`git diff --check`：通过。
- API `/health/ready=200`，Portal `=200`。

## 指标口径与后续

阶段 98 的 25 条冻结集仍为：路由 100%、期望工具 25/25 成功、关键词属性 60/71、P50 211ms、P95 20.16s。它不是生产用户指标。RAG 的 3 条合成排序集 Recall@1/3=100%、MRR/nDCG=1 也不能当作生产召回准确率。

下一阶段应：

1. 针对 `model generate` 建立按 profile、成功/超时/过载的 P50/P95/P99 与排队时间，但不能添加 tenant、prompt、模型名或 trace ID 标签。
2. 设定经业务确认的交互 SLO，并在独占容量环境中分别测试成功基线和 admission shedding；不能并行混跑后用全局计数判断幂等。
3. 扩充人工标注 RAG 集，独立报告 empty、error、citation correctness 与权限泄漏，避免把它们合并为“召回率”。
4. 将 Chat Trace 链接到受 RBAC 保护的详情页，并继续只展示脱敏摘要。
