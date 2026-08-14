# 阶段 212：服务 API Key Registry 与撤销/过期门禁

## 面试缺口

固定环境变量 Key、哈希 Key 和 scopes 已能满足单服务主体，但仍无法支持多个服务账号、独立撤销和到期控制。没有 Registry 时，平台不能证明某个 Key 的身份生命周期由数据库状态管理。

## 实现

新增可选 `ws_service_api_key` Registry：

- 只存 `key_hash`（SHA-256），不存原始 Secret；
- 绑定 `key_id`、tenant、service user、roles、scopes；
- 认证查询要求 `status=active` 且 `expires_at` 未到期；
- 记录失效、过期、缺少角色/scope 或数据库错误时 fail-closed；
- 通过 `SERVICE_API_KEY_REGISTRY_ENABLED=true` 或 `auth.service_api_key_registry_enabled` 显式启用；
- Registry 启用后不会回退到静态环境 Key，避免撤销记录被绕过；
- 默认保持关闭，便于先迁移数据库和 Secret Manager，再切换流量。

迁移：

```text
manifest/sql/migrations/20260814_service_api_key_registry.sql
```

## 验证

单元测试覆盖 Registry opt-in、roles/scopes JSON 边界和重复项；integration build-tag 测试覆盖 active 记录身份绑定及 revoked Key 拒绝。没有 `OPS_TEST_MYSQL_DSN` 时集成测试跳过，不虚构数据库执行结果。

## 剩余风险

尚未提供 Key 创建/轮换/撤销管理 API、last-used 写入策略、双人审批、Secret Manager 同步和多个服务主体的运营界面。Registry 启用前必须先执行迁移，并用专用服务账号完成跨租户资源级回归。
