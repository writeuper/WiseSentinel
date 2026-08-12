# 阶段 35：Index Worker DB 优先降级

## 问题根因

Index Worker 同时具备 MySQL `ClaimRunnable` token CAS 和 Redis `SETNX` 锁，但启动与派发都将 Redis 视为硬依赖。Redis 短暂不可用时，RAG 已就绪、MySQL 可用的情况下，所有待索引文档会停止推进。

这把缓存可用性错误提升为持久化任务正确性前置条件，也不符合企业 Agent 平台异步任务的 DB lease/CAS 基线。

## 方案与取舍

| 方案 | 结论 | 原因 |
| --- | --- | --- |
| Redis 不可用则不启动 Index Worker | 淘汰 | 无必要降低知识入库可用性 |
| 取消 Redis，但仅依赖 DB CAS | 可行但未采用 | 会失去多实例场景的低成本降载提示 |
| MySQL Claim/CAS 为正确性、Redis 可选 | 采用 | MySQL 原子领取围栏最终所有权；Redis 正常时仍减少无效竞争 |

## 实现与不变量

- Bootstrap 即使 Redis 不可达，也会创建完整 RAG 的 Index Worker；Ops Worker 仍保留 Redis 前提，未在本阶段改变其运行模型。
- `IndexWorker.dispatch`：
  - Redis client 为空时直接进入 `ClaimRunnable`；
  - `SETNX` 返回基础设施错误时记录脱敏 warning 并继续 DB Claim；
  - Redis 正常且锁被他人持有时才跳过本次派发；
  - Redis lock 的 renew/release 仅在确实获取到该锁后调用。
- MySQL `status=running + execution_token` 仍是 Execute/Finish 的最终围栏；无 Redis 不会让两个 Worker 同时完成同一任务。

## 自动化回归

新增真实 MySQL 集成用例：创建 pending index task，以 `NewIndexWorker(repo, nil, executor)` 派发；受控 executor 验证状态与 token 后用 `MarkFinishedIfOwned` 写 success。

```bash
OPS_TEST_MYSQL_DSN='mysql:ws:ws123@tcp(127.0.0.1:3306)/wisesentinel?charset=utf8mb4&parseTime=True&loc=Local' \
go test -tags=integration ./internal/orchestrator/task \
  -run 'Test(IndexLease|IndexWorkerExecutes)' -count=1 -v
```

结果：DB-only IndexWorker 成功执行并完成 CAS；原 Redis lock 测试在未配置 Redis 地址时按设计跳过。

## 剩余风险

- 当前 Index Task 的 DB lease 由 `started_at` 过期判断，尚未和 Vector GC 一样携带显式 lease expiry；长任务超过 stale window 的容量与重放需要继续压测。
- Ops Worker 的 Redis 硬依赖与 Index Worker 已不同；若需要 Ops 在缓存降级时持续执行，必须单独设计其超时、审批和状态机迁移，不能直接复制本阶段变更。
