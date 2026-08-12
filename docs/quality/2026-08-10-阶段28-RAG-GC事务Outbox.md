# 阶段 28：RAG GC 事务 Outbox

## 根因

generation 发布只切换 active pointer；旧 active generation 和 legacy 向量没有 durable 清理意图。若在 MySQL 发布后进程崩溃，或外部 Milvus 删除不可用，后续没有可审计、可重试的依据，遗留向量会持续占用存储。

## 企业级方案

采用 MySQL 事务 outbox：将“某个 generation/legacy 向量已过期”的清理意图与 active pointer 切换写入同一事务。唯一键 `(tenant_id, doc_id, target_key)` 让重复发布、重试和崩溃重放保持幂等。Milvus 物理删除将在独立 worker 中最终一致执行；读取仍由阶段 26/27 的 active state fail-closed 保护。

## 落地

- 新增 `ws_rag_vector_gc_task`：target、原因、状态、尝试次数、下次时间、lease token 和错误摘要字段，以及 target 去重/runnable 索引。
- 新增可重复 migration `20260810_rag_vector_gc_outbox.sql`，本地 MySQL 已应用。
- `PublishIfOwned` 锁定 state 后，在同一事务中：
  - 为被替代的非零 active generation 写入 `generation:<n>`；
  - 首次关闭 legacy 兼容时写入唯一的 `legacy:v1`；
  - 再切换 active pointer 与成功任务终态。
- GC worker 尚未接入；outbox 当前只保证意图不丢失，不能宣称已完成物理回收。

## 回归证据

```text
OPS_TEST_MYSQL_DSN='mysql:ws:***@tcp(127.0.0.1:3306)/wisesentinel?...' \
  go test -tags=integration ./internal/repository \
  -run TestPublishEnqueuesDeduplicatedVectorGCIntegration -count=1 -v
```

先新增测试，初始查询结果为空而失败；实现后通过，验证 G1→G2 产生且只产生 `generation:1`、`legacy:v1` 两条 pending outbox。

## 遗留项

- 需要 VectorGCWorker 的 MySQL lease/CAS、重试退避和 dead-letter 语义。
- generation/legacy/document_all 需要 tenant+doc 精确 Milvus 删除；legacy 无 generation metadata 时必须走受限 ID 扫描，不能将 `generation=0` 当作缺失字段的等价物。
- 文档删除应在同一事务写入 `document_all` outbox，并从同步外部删除切换为 durable eventual cleanup。
