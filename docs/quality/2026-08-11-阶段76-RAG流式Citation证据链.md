# 阶段 76：RAG 流式 Citation 证据链

日期：2026-08-11

## 问题与根因

Portal 已支持 `citation` SSE 事件，但 Chat Agent 的 Stream 路径此前忽略了 `retrieveDocs` 返回的 citations。同步 Chat 有引用，流式 Chat 没有，导致同一用户问题因传输模式不同而失去可验证的知识证据。

## 企业级方案与落地

- Chat Stream 在模型生成前发送每条检索 citation。即使模型后续失败，用户也能区分“检索命中了哪些受控知识”与“模型是否成功完成回答”。
- Handler 对 citation 使用与同步 Chat 相同的受控投影：保留 doc/chunk 标识，source/snippet 经过长度与敏感信息摘要处理；无效 citation payload 返回空对象，不把原始异常发送给浏览器。
- SSE writer 对多行 data 的每行加 `data:` 前缀，保障 citation/正文不会破坏 SSE framing。
- Portal 已在真实 SSE parser 中消费 citation frame 并复用现有引用展示组件。

## 真实生命周期回归

执行：上传唯一 marker 文档 → 等待索引成功 → 创建 session → SSE RAG 问答 → 验证 citation 的 `doc_id` → 精确删除 session 与文档。

结果：收到 `connected`、3 个 `citation`、多个 `message`、`done`；其中至少一条 citation 的 `doc_id` 与本次上传文档完全匹配。所有测试资源均在 finally 中删除。

## 门禁

- Chat/handler Go 测试通过，新增 citation 安全投影与多行 SSE 编码断言。
- Portal TypeScript + Vite production build 通过。
- 后端已重建部署并通过真实 RAG SSE 回归。

## 剩余风险

当前每个 citation 直接推送，TopK 增大时需要事件预算与前端去重策略；生产环境还需按角色/文档密级验证流式与同步接口的引用投影完全一致，并采集 citation 到达率、首 token 与首 citation 时延。
