# 阶段 113：Vector-GC 审批闭环与开发账号 RBAC 修复

## 发现与修复

### 1. Vector-GC 重驱请求校验规则无效

`RequestVectorGCRedriveReq.Reason` 使用了 GoFrame 不支持的 `required-length:1,500` 规则，所有合法重驱申请均被框架拦截并返回 400，导致死信无法进入审批闭环。

修复为 `required|length:1,500`，并增加 API tag 回归断言。

### 2. 管理员演示账号角色未生效

开发登录接口直接用邮箱查询 `ws_user_role.user_id`，而初始化数据把 `sreadmin@example.com`、`platformadmin@example.com` 存在 `ws_user.email`，对应稳定 user_id 分别为 `sre_admin`、`platform_admin`。因此两个管理员账号实际只获得 operator，无法访问审批中心。

修复登录角色解析：同时支持稳定 user_id 和邮箱映射到 user_id，保持默认 operator fallback。

## 真实闭环验证

使用两个不同管理员账号完成：

1. `sreadmin@example.com` 申请指定 `doc_id + target_key` 的 dead Vector-GC 重驱；
2. `platformadmin@example.com` 查看脱敏审批目标；
3. 第二管理员批准审批；
4. 原 dead 任务重新进入队列并被 Worker 执行；
5. 8 秒后该精确任务不再出现在 dead 列表。

实际结果：申请 `200/pending`，审批列表包含 `vector_gc_redrive` 目标投影，批准 `200/approved`，目标 dead 记录消失。未直接删除数据库记录或共享数据。

## 回归结果

- Go 全量测试：通过。
- Go race 重点包：通过。
- Python 自动化测试：27/27 通过。
- 敏感输出门禁：通过。
- 幂等重放：通过。
- 并发幂等：通过。
- SSE 基础：通过。
- Trace 访问：通过。
- readiness：MySQL、Redis、Milvus、RAG、模型和 Worker 全部 up。

## 当前指标快照

当前本地集成后端生命周期：

- 已结束 Trace：1194；成功/完成：1138；失败或终态错误：56。
- 成功 Agent 平均耗时：6802.65ms。
- 全部已结束平均耗时：6751.37ms。
- 平均 Trace 步数：3.397；最大：51。
- 工具调用成功率：90.47%（1814/2005）。
- active 文档：24；已发布逻辑 Chunk：78；Milvus 物理向量：198。
- Vector-GC：dead 27、succeeded 67；本轮成功消化 1 个 default 租户 dead 任务。
- 当前进程 RAG 样本为 0；未将其解释为 RAG P95。RAG P95 必须在重新积累足够成功样本后报告。

## 剩余风险

- 仍有 27 个 dead 任务，其中历史集成租户任务需按租户和目标逐项治理；禁止批量删除或修改状态掩盖问题。
- 当前无真实生产用户或生产流量证据；以上仅为本地/集成环境结果，不构成生产 SLA。
