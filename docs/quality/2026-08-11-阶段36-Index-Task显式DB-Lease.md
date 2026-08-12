# 阶段 36：Index Task 显式 DB Lease

## 问题根因

阶段 35 将 Index Worker 从 Redis 硬依赖中解耦，但 MySQL 对 running task 的回收仍依赖 `started_at < staleBefore`。该字段同时表示开始时间和隐式租约，无法表达“任务正在运行且已续约”；长索引任务可能被错误接管，或将错误的 stale window 固化到所有任务。

## 企业级设计

- 在 `ws_index_task` 增加 `lease_expires_at` 和 runnable 索引，使用可重复 migration `20260811_index_task_db_lease.sql`。
- `pending` 可领取；`running` 仅在 lease NULL（迁移前兼容）或到期后可领取。
- Claim 写入 `execution_token + lease_expires_at`；`RenewLeaseIfOwned` 必须同时满足 running、token 匹配和当前 lease 尚未到期。
- Finish、Publish 和 Service Execute 都要求 token 与未到期 lease；终态/Cancellation 清空 lease。
- Index Worker 运行期间每 lease TTL 的三分之一续约 DB lease；续约失败取消执行上下文，令过期任务由其他 Worker 安全接管。

Redis 锁仍只承担可选降载。无 Redis 时，DB lease/token 独立确保同一 task 的最终状态不能被 stale owner 覆盖。

## 兼容与迁移

- migration 检查 information_schema 后再增加 column/index，可安全重复运行。
- 历史 running 行为 `lease_expires_at=NULL`，会在下一次扫描时变为可领取，不会永久卡住。
- 新 schema 的 `idx_index_running` 与新增 `idx_index_lease_runnable` 都支持按状态/过期时间扫描；旧索引保留，避免部署期间依赖方中断。

## 自动化回归

| 场景 | 断言 | 结果 |
| --- | --- | --- |
| 初始 Claim | pending 写入 token 与未来 lease | 通过 |
| 未过期重复 Claim | 第二 owner 不能领取 | 通过 |
| 到期接管 | token B 成功接管，token A 无法终态完成 | 通过 |
| stale renew | token A 不能延长已接管任务 | 通过 |
| owner renew | token B 能延长有效 lease | 通过 |
| 真实 RAG 增量索引 | G1/G2 publish 与读取回归 | 通过 |

执行证据：

```bash
OPS_TEST_MYSQL_DSN='mysql:ws:ws123@tcp(127.0.0.1:3306)/wisesentinel?charset=utf8mb4&parseTime=True&loc=Local' \
go test -tags=integration ./internal/repository \
  -run 'Test(IndexTask|DocumentIndexGeneration|PublishEnqueues)' -count=1 -v

OPS_TEST_MYSQL_DSN='mysql:ws:ws123@tcp(127.0.0.1:3306)/wisesentinel?charset=utf8mb4&parseTime=True&loc=Local' \
go test -tags=integration ./internal/rag -run '^TestPipelineIncrementalReindex$' -count=1 -v
```

## 剩余风险

- 当前 lease TTL 固定为 2 分钟；应在生产按文档大小、embedding latency 和 Milvus SLO 配置化，并监控续约失败率。
- DB lease renewal 只在 Index Worker 进程内工作；部署优雅退出需要保留足够 termination grace period，让运行 context 取消和 lease 自然接管一致。
