# 阶段 168：审批决定 RowsAffected 错误传播

## 发现

审批拒绝/终止流程在数据库 `UPDATE` 成功返回后读取 `RowsAffected()` 时，原实现忽略了该错误，并固定返回 `nil`。在驱动或连接异常场景下，上层可能将“无法确认是否更新”的状态误判为普通的未命中，造成审批状态、审计结果和告警不一致。

## 修复

- `ApprovalRepo.Decide` 现在向上返回 `RowsAffected()` 错误。
- 保留现有 CAS 条件（租户、审批 ID、pending 状态和有效期），并继续禁止没有专用执行器的通用 approval 直接标记为 approved。

## 验证

- `go test ./internal/repository ./internal/gateway/handler ./internal/orchestrator/task`：通过。
- `go vet ./...`：通过。
- 集成测试仍按 `OPS_TEST_MYSQL_DSN` 配置选择性运行；未配置时不会虚报真实数据库验证结果。

## 影响与剩余风险

数据库写入结果现在不会因影响行数读取失败而被静默吞掉，调用方可进入统一错误处理和重试/告警路径。当前未在本环境强制启动独立 MySQL 集成租户，生产级并发审批指标仍需在线环境按既有集成脚本采集。
