# 阶段 211：Service API Key 哈希校验与轮换窗口

## 面试缺口

服务 API Key 已具备固定 tenant、roles 和 scopes，但仅支持明文 Secret 配置，无法满足 Secret Manager 只注入摘要、轮换期间双 Key 并存和应用侧不持有可回显密钥的要求。

## 实现

- 支持 `SERVICE_API_KEY_SHA256` 或 `auth.service_api_key_sha256`；
- 支持 `SERVICE_API_KEY_PREVIOUS_SHA256` 或对应配置，作为有限轮换窗口的旧 Key；
- 仍兼容明文 `SERVICE_API_KEY`，便于开发/迁移，但生产建议只配置哈希；
- 使用 SHA-256 + 常量时间比较，错误长度/非法十六进制直接拒绝；
- 不把原始 Key、哈希或配置内容写入 Trace、审计和日志；
- tenant/user/roles/scopes 仍由服务配置绑定，不能由请求头覆盖。

## 验证

专项测试覆盖当前哈希、previous 哈希、错误 Key、非法摘要和 rotation 双 Key 窗口。完整 Go、Python 和迁移门禁沿用本阶段执行结果。

## 剩余风险

这仍不是完整 API Key Registry：尚未实现 DB 哈希存储、Key ID、撤销时间、自动轮换控制器、多个服务主体和泄露告警。生产上线前应接入 Secret Manager/IdP，并限制 previous Key 的有效期。
