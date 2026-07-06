# WiseSentinel Platform

智哨（WiseSentinel）企业级智能运维 Agent 平台 — Phase 1 模块化单体。

Phase 1 全部里程碑（M1-M5）已实施完毕：模块化单进程 + 单 Portal。详细的进度对照见 [`docs/WiseSentinel-企业级智能运维Agent平台-详细设计.md`](docs/WiseSentinel-企业级智能运维Agent平台-详细设计.md) 第 2 章架构与第 17 章任务清单。

---

## 已完成里程碑

| 里程碑 | 内容 |
|--------|------|
| **M1 基础框架** | GoFrame 服务、M1-M5 全套 Gateway 中间件链、MySQL/Redis/Milvus 客户端、`/health/*` + `/metrics` |
| **M2 RAG + Knowledge** | DashScope Embedding、Milvus 2048-dim FloatVector、Knowledge Eino Graph、上传/索引/删除 API |
| **M3 Chat Agent** | Eino v0.6.0 ReAct Agent、SessionService（Redis+MySQL）、ToolGateway 4 个 L0 工具、Chat 同步+SSE 流式、`/me` 身份端点 |
| **M4 Ops Agent** | Eino v0.6.0 `planexecute` Plan-Execute-Replan、ops_plan + ops_exec 双 profile、Ops Worker（DB 轮询 + Redis 分布式锁） |
| **M5 Portal + RBAC** | React Portal 4 个页面（Chat/Ops/Knowledge/Approvals/Admin）、角色感知菜单、RBAC 种子数据 |

---

## 1. 环境准备

### 1.1 基础设施

通过 `manifest/docker/docker-compose.yml` 一键启动 MySQL / Redis / Milvus / MinIO / etcd：

```bash
cd manifest/docker
docker compose up -d
# 第一次启动会自动执行 manifest/sql/schema.sql 创建表 + 种子数据
```

> 仅当 MySQL 数据卷为空时才会自动执行 init 脚本。如需重建，请 `docker compose down -v` 删除卷后重新 `docker compose up -d`。

手动初始化：

```bash
mysql -h 127.0.0.1 -u ws -pws123 wisesentinel < manifest/sql/schema.sql
```

### 1.2 API Key / 配置

```bash
cp .env.example .env
set -a && source .env && set +a
```

最小配置（启动后端只需要 JWT_SECRET 即可，开发 Token 会用 defaults；下面两项是 Chat / Ops 真正能完成端到端 LLM 调用所必须）：

```ini
JWT_SECRET=change-me-in-production
LLM_API_KEY=sk-xxxxxxxxxxxxxxxxxxxx          # OpenAI 兼容 / DeepSeek / Volcengine Ark 等
LLM_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
LLM_MODEL=deepseek-v3-2-251201                # 模型名
EMBED_API_KEY=sk-xxxxxxxxxxxxxxxxxxxx         # DashScope text-embedding-v4
```

`manifest/config/config.yaml` 给出本地开发默认值；**生产仅用环境变量覆盖**，无需修改 yaml。

| 变量 | 作用 | 默认 |
|------|------|------|
| `JWT_SECRET` | JWT 签名密钥 | dev 默认值（生产必须改） |
| `DEV_PASSWORD` | 开发账号密码 | `dev123` |
| `DEV_API_KEY` | 开发 API Key | `ws-dev-key` |
| `MYSQL_DSN` | MySQL 连接字符串 | 本地 3306 |
| `REDIS_ADDRESS` / `REDIS_PASSWORD` | Redis 连接 | 127.0.0.1:6379 |
| `MILVUS_ADDRESS` | Milvus gRPC | 127.0.0.1:19530 |
| `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` | Chat / Ops 调用 | 空 |
| `EMBED_API_KEY` | DashScope Embedding | 空 |
| `PROMETHEUS_URL` | Prometheus 告警查询 | http://127.0.0.1:9090 |
| `MCP_LOG_URL` | MCP 日志查询 | 本地默认 |

---

## 2. 启动后端

```bash
cd /home/ubuntu/WiseSentinel
go run ./cmd/platform
```

启动成功后会输出（中间件齐全后）：

```
[INFO] mysql ping OK
[INFO] redis ping OK
[INFO] milvus ping OK
[INFO] OpsWorker started
[INFO] server started at :8090
```

健康检查：

```bash
curl http://127.0.0.1:8090/health/live      # 存活
curl http://127.0.0.1:8090/health/ready     # MySQL/Redis/Milvus 连通
curl http://127.0.0.1:8090/metrics | grep ws_http
```

---

## 3. 启动前端

```bash
cd portal
npm install
npm run dev
# → http://127.0.0.1:5173
```

首次访问会自动跳转到 `/login`。开发期默认密码 `dev123`，测试账号见下表：

| 用户名 | 角色 | 可访问页面 |
|--------|------|------------|
| `sre@example.com` | viewer · operator · sre_admin · platform_admin | **全部**（建议用此账号演示） |
| `viewer@example.com` | viewer | 仅 Chat / Knowledge |
| `operator@example.com` | operator · viewer | Chat / Knowledge / Ops |
| `sreadmin@example.com` | sre_admin · operator · viewer | + Approvals / Admin |
| `platformadmin@example.com` | platform_admin · sre_admin · operator · viewer | 同上 |

任意账号密码都是 `dev123`（可由 `DEV_PASSWORD` 覆盖）。

---

## 4. 端到端验证

### 4.1 获取 Token

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8090/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"username":"sre@example.com","password":"dev123"}' \
  | jq -r '.data.access_token')
echo "TOKEN=$TOKEN"
```

### 4.2 验证 /me 携带真实角色

```bash
curl -s http://127.0.0.1:8090/api/v1/me -H "Authorization: Bearer $TOKEN" | jq
# {"username":"sre@example.com","tenant_id":"default","roles":["viewer","operator","sre_admin","platform_admin"]}
```

### 4.3 M2 知识库

```bash
# 上传 Markdown
curl -X POST http://127.0.0.1:8090/api/v1/knowledge/documents/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@testdata/knowledge/alert_runbook.md"

# 文档列表
curl http://127.0.0.1:8090/api/v1/knowledge/documents \
  -H "Authorization: Bearer $TOKEN" | jq
```

### 4.4 M3 Chat（RAG + 工具）

```bash
SESSION=$(curl -s -X POST http://127.0.0.1:8090/api/v1/sessions \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"title":"test"}' | jq -r '.data.session_id')

curl -s -X POST http://127.0.0.1:8090/api/v1/chat \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"session_id\":\"$SESSION\",\"question\":\"服务下线告警怎么处理？\"}" | jq

# 流式
curl -sN -X POST http://127.0.0.1:8090/api/v1/chat/stream \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"session_id\":\"$SESSION\",\"question\":\"请列出排查步骤\"}"
```

### 4.5 M4 Ops（Plan-Execute-Replan）

```bash
# 同步
curl -s -X POST http://127.0.0.1:8090/api/v1/ops/analyze \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"query":"分析当前告警并给出处置建议","options":{"async":false,"max_iterations":20}}' | jq

# 异步（Web 端走 OpsWorker DB 轮询）
curl -s -X POST http://127.0.0.1:8090/api/v1/ops/analyze \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"query":"分析当前告警","options":{"async":true,"max_iterations":20}}' | jq

# 任务列表
curl http://127.0.0.1:8090/api/v1/ops/tasks -H "Authorization: Bearer $TOKEN" | jq

# 查询单个任务
curl http://127.0.0.1:8090/api/v1/ops/tasks/<task_id> \
  -H "Authorization: Bearer $TOKEN" | jq
```

### 4.6 M5 Approvals / Admin

```bash
# 待审批列表（需要 sre_admin+）
curl http://127.0.0.1:8090/api/v1/approvals -H "Authorization: Bearer $TOKEN" | jq

# Agent 配置列表
curl 'http://127.0.0.1:8090/api/v1/admin/agent-configs?agent_type=chat' \
  -H "Authorization: Bearer $TOKEN" | jq

# 激活某个版本
curl -X PUT http://127.0.0.1:8090/api/v1/admin/agent-configs/v2-beta/activate \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"agent_type":"chat"}' | jq
```

### 4.7 集成测试

```bash
go test ./internal/rag/ -v
go test ./internal/agent/knowledge/ -v
```

---

## 5. 前后端如何协同

```
Portal (:5173, Vite dev)
   │ HTTPS / http (dev)
   ▼
GoFrame API (:8090)
   ├── /api/v1/*        (RBAC + Audit + RateLimit + Trace)
   ├── /health/*        (/health/live, /health/ready)
   └── /metrics         (Prometheus ws_http_*)

Pipeline:
   Login(/api/v1/auth/token) ─► JWT in localStorage
       │
       ▼
   任意请求带 Authorization: Bearer <jwt>
       │
       ▼
   AuthMiddleware → roles → RBAC 矩阵 → handler
```

Portal 默认通过 `import.meta.env.VITE_API_BASE` 拼出 `/api/v1` 前缀；通过 Vite proxy 转发到 `:8090`。如需绕过 proxy 直接调用，可在 `portal/.env` 设置 `VITE_API_BASE=http://localhost:8090/api/v1`。

---

## 6. 关键设计文档

- 详细设计文档：[docs/WiseSentinel-企业级智能运维Agent平台-详细设计.md](docs/WiseSentinel-企业级智能运维Agent平台-详细设计.md)
- API 契约：`api/v1/*.go`
- 数据库 Schema：`manifest/sql/schema.sql`
- 配置文件：`manifest/config/config.yaml`

---

## 7. 开发提示

- `go build -mod=mod ./cmd/platform` 编译后端；`portal/src` 用 `npm run dev` / `npm run build`。
- 修改 `manifest/config/config.yaml` 后无需重启，GoFrame 默认会热加载。
- 任何 `/api/v1/ops/tasks` 长时间停留 `pending`：确认 Redis 可达，否则 OpsWorker 不会启动。
- LLM 报错 "node not exist"：检查 `LLM_MODEL` 与 `LLM_BASE_URL` 是否匹配你的供应商（DeepSeek / 火山方舟 / OpenAI 等的模型名和 base URL 不同）。

---

## 8. 后续（Phase 2+）

| 项 | 备注 |
|----|------|
| OpenTelemetry Span | 当前为 `X-Trace-ID` 字符串 |
| Kafka 事件驱动（Ops） | 当前为 DB 轮询 Worker |
| Langfuse Trace | Phase 2 优先 |
| L2 工具 + 完整审批流 | 当前 L0 工具集，L1 query_logs 已实现 |
| E2E 测试矩阵 | Playwright / Go e2e 套件 |
