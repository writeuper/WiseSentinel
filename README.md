# WiseSentinel Platform

智哨（WiseSentinel）企业级智能运维 Agent 平台 — Phase 1 模块化单体。

## M2 里程碑（当前）

- RAG Engine：DashScope Embedding（无密钥时回退 HashEmbedder）+ Milvus 索引/检索
- Knowledge Eino Graph：`FileLoader → MarkdownSplitter → MilvusIndexer`（`internal/agent/knowledge`）
- Knowledge 上传 API：文档存储、同步索引、`ws_document` / `ws_index_task` 落库
- 增量索引：按 `_source` / `doc_id` 删除旧向量后重建
- 租户过滤检索：`metadata["tenant_id"]` + `visibility` + `secret_level`（按角色）

## M1 里程碑

- 可启动的 GoFrame 服务（`:8090`）
- Gateway 中间件链：Recovery / Trace / CORS / Tenant / JWT+API Key / RBAC / RateLimit / Audit
- MySQL DDL、Redis、Milvus 配置化客户端（支持环境变量覆盖）
- Health（`/health/live`、`/health/ready`）与 Metrics（`/metrics`，含 `ws_http_requests_total`）
- API v1 契约骨架（Chat / Ops / Knowledge / Session / Admin）

## 快速启动

### 1. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env，至少设置 JWT_SECRET；本地开发可保留默认值
set -a && source .env && set +a
export GF_GCFG_PATH=manifest/config
```

`manifest/config/config.yaml` 提供本地开发默认值；生产环境通过环境变量覆盖（见 `internal/bootstrap/envconfig.go`），**无需修改 yaml**：

| 变量 | 覆盖的配置项 | 默认值（yaml） |
|------|--------------|----------------|
| `JWT_SECRET` | `auth.jwt_secret` | `change-me-in-production` |
| `DEV_API_KEY` | `auth.dev_api_key` | `ws-dev-key` |
| `DEV_PASSWORD` | `auth.dev_password` | `dev123` |
| `MYSQL_DSN` | `database.default.link` | 本地 `127.0.0.1:3306` |
| `REDIS_ADDRESS` | `redis.default.address` | `127.0.0.1:6379` |
| `REDIS_PASSWORD` | `redis.default.pass` | 空 |
| `MILVUS_ADDRESS` | `milvus.address` | `127.0.0.1:19530` |
| `LLM_API_KEY` / `LLM_BASE_URL` | 模型 profile | 空（M3 起使用） |
| `EMBED_API_KEY` | embedding profile | 空（M2 起使用） |

### 2. 启动基础设施

```bash
cd manifest/docker
docker compose up -d mysql redis milvus
```

**数据库 Schema 初始化**：首次启动 MySQL 容器时，`manifest/sql/schema.sql` 会通过 `docker-entrypoint-initdb.d` 自动执行，创建全部 `ws_*` 表及默认租户种子数据。

> 注意：仅当 MySQL 数据卷为空时才会执行 init 脚本。若需重建，请先删除对应 Docker volume 后重新 `docker compose up`。

手动初始化（不使用 Docker 或已有 MySQL 实例时）：

```bash
mysql -h 127.0.0.1 -u ws -pws123 wisesentinel < manifest/sql/schema.sql
```

### 3. 本地运行

```bash
cd /path/to/WiseSentinel
export GF_GCFG_PATH=manifest/config
go run ./cmd/platform
```

### 4. 验证

```bash
# 存活探针
curl http://127.0.0.1:8090/health/live

# 就绪探针（MySQL + Redis + Milvus）
curl http://127.0.0.1:8090/health/ready

# Prometheus 指标（含 ws_http_requests_total）
curl http://127.0.0.1:8090/metrics | grep ws_http

# 获取开发 Token
curl -X POST http://127.0.0.1:8090/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"username":"sre@example.com","password":"dev123"}'

# 带 JWT 调用 Ping
curl http://127.0.0.1:8090/api/v1/ping \
  -H "Authorization: Bearer <token>"
```

## Trace 说明（M1）

M1 阶段使用轻量级 `X-Trace-ID` 请求关联（Gateway 中间件注入/透传），贯穿日志与审计字段。OpenTelemetry Span 埋点计划在 M3（Agent 链路）接入。

## 项目结构

详见 [详细设计文档](docs/WiseSentinel-企业级智能运维Agent平台-详细设计.md) 第 2 章。

## 后续里程碑

| 里程碑 | 内容 |
|--------|------|
| M3 | Chat Agent + Redis 会话 |
| M4 | Ops Agent + Prometheus |
| M5 | Portal + E2E 验收 |

## M5 Portal 前端（React）

```bash
cd portal && npm install && npm run dev
# http://127.0.0.1:5173 · 知识库已对接 M2 API
```

详见 [`portal/README.md`](../portal/README.md)。

### M2 知识库验证

```bash

# 模拟获取token
TOKEN=$(curl -s -X POST http://127.0.0.1:8090/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"username":"sre@example.com","password":"dev123"}' \
  | jq -r '.data.access_token')

echo $TOKEN

curl -X POST http://127.0.0.1:8090/api/v1/knowledge/documents/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@testdata/knowledge/alert_runbook.md"

# 获取 Token 后上传 Runbook
curl -X POST http://127.0.0.1:8090/api/v1/knowledge/documents/upload \
  -H "Authorization: Bearer <token>" \
  -F "file=@testdata/knowledge/alert_runbook.md"

# 查询索引任务
curl http://127.0.0.1:8090/api/v1/knowledge/index-tasks/<task_id> \
  -H "Authorization: Bearer <token>"

# 文档列表
curl http://127.0.0.1:8090/api/v1/knowledge/documents \
  -H "Authorization: Bearer <token>"
```

集成测试（需 Milvus）：

```bash
GF_GCFG_PATH=manifest/config go test -tags=integration ./internal/rag/ -v
```

覆盖：索引/检索闭环、Eino Pipeline 增量重索引、租户隔离、`secret_level` 过滤。
