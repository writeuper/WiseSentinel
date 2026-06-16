# WiseSentinel Platform

智哨（WiseSentinel）企业级智能运维 Agent 平台 — Phase 1 模块化单体。

## M1 里程碑（当前）

- 可启动的 GoFrame 服务（`:8080`）
- Gateway 中间件链：Recovery / Trace / CORS / Tenant / JWT+API Key / RBAC / RateLimit / Audit
- MySQL DDL、Redis、Milvus 配置化客户端
- Health（`/health/live`、`/health/ready`）与 Metrics（`/metrics`）
- API v1 契约骨架（Chat / Ops / Knowledge / Session / Admin）

## 快速启动

### 1. 启动基础设施

```bash
cd manifest/docker
docker compose up -d mysql redis milvus
```

### 2. 本地运行

```bash
cd /home/ubuntu/WiseSentinel
export GF_GCFG_PATH=manifest/config
go run ./cmd/platform
```

### 3. 验证

```bash
# 存活探针
curl http://127.0.0.1:8080/health/live

# 获取开发 Token
curl -X POST http://127.0.0.1:8080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"username":"sre@example.com","password":"dev123"}'

# 带 JWT 调用 Ping
curl http://127.0.0.1:8080/api/v1/ping \
  -H "Authorization: Bearer <token>"
```

## 项目结构

详见 [详细设计文档](docs/WiseSentinel-企业级智能运维Agent平台-详细设计.md) 第 2 章。

## 后续里程碑

| 里程碑 | 内容 |
|--------|------|
| M2 | RAG + Knowledge 索引闭环 |
| M3 | Chat Agent + Redis 会话 |
| M4 | Ops Agent + Prometheus |
| M5 | Portal + E2E 验收 |
