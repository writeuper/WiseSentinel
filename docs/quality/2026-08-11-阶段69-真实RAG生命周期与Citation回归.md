# 阶段 69：真实 RAG 生命周期与 Citation 回归

日期：2026-08-11

## 目标

Chat 与 Ops 回归不能证明知识库写入、异步索引、检索引用和删除读侧围栏正确。本轮在隔离评测 tenant 执行 `ROUTE-004`，使用每次运行唯一 marker，避免与历史文档或向量混淆。

## 场景与断言

1. multipart 上传临时 Markdown 文档；
2. 轮询专属 index task 至 success 且 chunk_count > 0；
3. 使用 RAG Chat 检索 marker，citation 必须关联本次 doc_id，且回答/citation 含 marker 与流程内容；
4. 精确删除该 doc_id；
5. 文档列表不得再出现该 doc_id。

评测产物只记录聚合结果和受控成功状态，不保存模型全文、文档正文或密钥。

## 真实结果

```bash
python3 scripts/run_agent_eval.py ... --case ROUTE-004 \
  --tenant-id rag-eval-stage69 --knowledge-tenant-id rag-eval-stage69-knowledge
```

通过：route=knowledge、上传/索引/RAG citation/删除四步均 success、knowledge hit 1/1、关键词 4/4、端到端 10.461s。Prometheus 记录一次 `ws_rag_retrievals_total{outcome=success,confidence=mid}`，检索耗时 0.423s。

## 结论与范围

当前真实依赖链路可完成最小 RAG 生命周期，删除后 API 列表读侧不再展示文档。该单次 canary 不能替代召回质量统计、跨租户对抗、密级过滤、代际 GC backlog 或大文档并发索引压测；这些仍需使用人工标注扩展集和隔离环境执行。删除后向量物理回收是异步 GC 责任，不能以本次 API 删除成功推断 Milvus 已立即清空。
