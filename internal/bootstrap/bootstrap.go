package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	chatagent "wisesentinel-platform/internal/agent/chat"
	"wisesentinel-platform/internal/agent/knowledge"
	opsagent "wisesentinel-platform/internal/agent/ops"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/memory"
	"wisesentinel-platform/internal/model"
	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/orchestrator/router"
	"wisesentinel-platform/internal/orchestrator/task"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/repository"
	"wisesentinel-platform/internal/toolkit"
	"wisesentinel-platform/internal/toolkit/adapters"
	mcpclient "wisesentinel-platform/internal/toolkit/mcp"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/redis/go-redis/v9"
)

// App holds shared infrastructure clients initialized at startup.
type App struct {
	Milvus             *client.MilvusClient
	RAG                domain.RAGService
	Documents          *repository.DocumentRepo
	Storage            *storage.LocalStore
	Memory             domain.SessionService // 会话内存
	ModelRouter        domain.ModelRouter    // 模型路由
	Toolkit            domain.ToolGateway    // 工具网关
	ChatAgent          domain.ChatAgent      // 聊天智能体
	OpsAgent           domain.OpsAgent       // Ops智能体
	IntentRouter       domain.IntentRouter
	SessionRepo        *repository.SessionRepo
	ChatTurnRepo       *repository.ChatTurnRepo
	OpsTaskRepo        *repository.OpsTaskRepo
	AlertEventRepo     *repository.AlertEventRepo
	ApprovalRepo       *repository.ApprovalRepo
	TraceRepo          *repository.AgentTraceRepo
	FaultKnowledgeRepo *repository.FaultKnowledgeRepo
	ToolCallRecordRepo *repository.ToolCallRecordRepo
	FeedbackRepo       *repository.FeedbackRepo
	OpsWorker          *task.OpsWorker
	IndexWorker        *task.IndexWorker
	VectorGCWorker     *task.VectorGCWorker
	AlertEventReaper   *task.AlertEventReaper
	VectorGCRepo       *repository.VectorGCRepo
	MCPTimeClient      *mcpclient.StdioClient
}

const (
	defaultMCPTimeInitTimeout = 2 * time.Second
	minMCPTimeInitTimeout     = 250 * time.Millisecond
	maxMCPTimeInitTimeout     = 10 * time.Second
	readinessProbeTimeout     = 2 * time.Second
)

// mcpTimeInitTimeout bounds startup time spent on the optional MCP time
// service. An unavailable optional integration must not make readiness wait
// for an unbounded external process startup.
func mcpTimeInitTimeout(ctx context.Context) time.Duration {
	raw := strings.TrimSpace(configx.String(ctx, "mcp.time.init_timeout_ms", "MCP_TIME_INIT_TIMEOUT_MS"))
	milliseconds, err := strconv.Atoi(raw)
	if err != nil || milliseconds < int(minMCPTimeInitTimeout/time.Millisecond) || milliseconds > int(maxMCPTimeInitTimeout/time.Millisecond) {
		return defaultMCPTimeInitTimeout
	}
	return time.Duration(milliseconds) * time.Millisecond
}

// Init wires database, cache, vector store, and all business services.
func Init(ctx context.Context) (*App, error) {
	// 从环境变量加载配置项
	applyConfigFromEnv(ctx)
	if err := validateRuntimeSecurity(ctx); err != nil {
		return nil, fmt.Errorf("runtime security validation failed: %w", err)
	}

	// 检查数据库连接
	if err := pingMySQL(ctx); err != nil {
		g.Log().Warning(ctx, "MySQL not ready:", err)
	}
	if err := pingRedis(ctx); err != nil {
		g.Log().Warning(ctx, "Redis not ready:", err)
	}

	// 初始化存储存储
	store := storage.NewLocalStore(ctx)
	docRepo := repository.NewDocumentRepo()
	taskRepo := repository.NewIndexTaskRepo()
	vectorGCRepo := repository.NewVectorGCRepo()
	sessionRepo := repository.NewSessionRepo()
	chatTurnRepo := repository.NewChatTurnRepo()
	opsTaskRepo := repository.NewOpsTaskRepo()
	alertEventRepo := repository.NewAlertEventRepo()
	if reaped, reapErr := alertEventRepo.ReapOrphanReservations(ctx, 5*time.Minute); reapErr != nil {
		g.Log().Warning(ctx, "orphan Alertmanager reservation reap failed:", reapErr)
	} else if reaped > 0 {
		observability.ObserveAlertEventOrphanReaped(reaped)
		g.Log().Infof(ctx, "reaped orphan Alertmanager reservations: %d", reaped)
	}
	alertEventReaper := task.NewAlertEventReaper(alertEventRepo)

	// Model Router
	modelRouter := model.NewRouter(ctx)

	// Tool Gateway
	toolGateway := toolkit.NewGateway(ctx)
	var mcpTimeClient *mcpclient.StdioClient
	var err error
	if configx.String(ctx, "mcp.time.enabled", "MCP_TIME_ENABLED") == "true" {
		mcpCtx, cancel := context.WithTimeout(ctx, mcpTimeInitTimeout(ctx))
		capabilityPolicy := &mcpclient.CapabilityPolicy{
			ExpectedToolNames: g.Cfg().MustGet(ctx, "mcp.time.expected_tool_names", []string{}).Strings(),
			MaxTools:          g.Cfg().MustGet(ctx, "mcp.time.max_tools", 0).Int(),
		}
		if len(capabilityPolicy.ExpectedToolNames) == 0 && capabilityPolicy.MaxTools <= 0 {
			capabilityPolicy = nil
		}
		mcpTimeClient, err = mcpclient.NewStdioClientWithOptions(mcpCtx,
			g.Cfg().MustGet(ctx, "mcp.time.command").String(),
			g.Cfg().MustGet(ctx, "mcp.time.args").Strings(),
			mcpclient.StartOptions{
				AllowedCommands:      g.Cfg().MustGet(ctx, "mcp.time.allowed_commands").Strings(),
				AllowedArgs:          g.Cfg().MustGet(ctx, "mcp.time.allowed_args").Strings(),
				EnvironmentAllowlist: g.Cfg().MustGet(ctx, "mcp.time.environment_allowlist").Strings(),
				CapabilityPolicy:     capabilityPolicy,
			},
		)
		cancel()
		if err != nil {
			g.Log().Warning(ctx, "MCP time server unavailable, degrading:", err)
		} else {
			toolGateway.SetMCPTimeAdapter(mcpclient.NewTimeAdapterWithMaxResponseBytes(
				mcpTimeClient,
				g.Cfg().MustGet(ctx, "mcp.time.max_response_bytes", 64*1024).Int(),
			))
		}
	}

	// Approval repository serves the existing durable approval workflows (for
	// example Vector GC). Generic L2 tool execution is fail-closed until the
	// dedicated intent/outbox executor is available.
	approvalRepo := repository.NewApprovalRepo()
	traceRepo := repository.NewAgentTraceRepo()
	if reaped, reapErr := traceRepo.ReapStaleRunning(ctx, 10*time.Minute); reapErr != nil {
		g.Log().Warning(ctx, "stale Agent Trace reap failed:", reapErr)
	} else if reaped > 0 {
		observability.ObserveStaleTraceReaped(reaped)
		g.Log().Infof(ctx, "reaped stale Agent Traces: %d", reaped)
	}
	faultKnowledgeRepo := repository.NewFaultKnowledgeRepo()
	toolCallRecordRepo := repository.NewToolCallRecordRepo()
	feedbackRepo := repository.NewFeedbackRepo()
	toolGateway.SetToolCallRecordRepo(toolCallRecordRepo)

	milvusClient, err := client.NewMilvusClient(ctx)
	if err != nil {
		g.Log().Warning(ctx, "Milvus not ready:", err)
	}

	var ragService domain.RAGService
	ragIndexingEnabled := false
	var vectorGCWorker *task.VectorGCWorker
	if milvusClient != nil {
		emb, embErr := embedder.NewFromConfig(ctx)
		if embErr != nil {
			g.Log().Warning(ctx, "embedder init failed:", embErr)
		} else {
			idx := indexer.NewMilvusIndexer(milvusClient, emb)
			retr := retriever.NewMilvusRetriever(milvusClient, emb)
			pipeline, pipeErr := knowledge.NewPipeline(ctx, store, idx)
			if pipeErr != nil {
				g.Log().Warning(ctx, "knowledge pipeline init failed:", pipeErr)
			} else {
				ragService = rag.NewService(pipeline, retr, idx, taskRepo)
				ragIndexingEnabled = true
				vectorGCWorker = task.NewVectorGCWorker(vectorGCRepo, repository.NewDocumentIndexStateRepo(), idx)
				// Only advertise internal-document retrieval when the complete RAG
				// pipeline is ready. The global adapter alone is insufficient: the
				// model's tool catalogue must reflect degraded capabilities too.
				adapters.SetRAGServiceForInternalDocs(ragService)
				toolGateway.EnableInternalDocsAdapter()
			}
		}
	}
	if ragService == nil {
		g.Log().Warning(ctx, "RAG degraded: document uploads will be stored without vector indexing")
		ragService = rag.NewService(nil, nil, nil, taskRepo)
	}

	// Memory / Session Service
	sessionService := memory.NewRedisSessionStore(sessionRepo)

	// Intent Router — use confidence-aware routing when RAG is available,
	// otherwise fall back to pure rule-based routing (degraded mode).
	var intentRouter domain.IntentRouter = router.NewRuleRouter()
	if ragService != nil {
		intentRouter = router.NewConfidenceRouter(ragService)
	}

	// Chat Agent
	chatAgent := chatagent.NewAgent(modelRouter, ragService, toolGateway, traceRepo)

	// Ops Agent (M4 — full Plan-Execute-Replan)
	opsAgent := opsagent.NewAgent(modelRouter, toolGateway, opsTaskRepo, faultKnowledgeRepo, traceRepo)

	// Async workers (DB polling with distributed locks)
	// Build a standalone *redis.Client from the same REDIS_ADDRESS env
	// so the worker can call distributed-lock primitives directly
	// without depending on GoFrame's adapter abstraction.
	var opsWorker *task.OpsWorker
	var indexWorker *task.IndexWorker
	var addr string
	if cfgAddr, _ := g.Cfg().Get(ctx, "redis.address"); cfgAddr != nil {
		addr = strings.TrimSpace(cfgAddr.String())
	}
	if strings.EqualFold(addr, "none") || strings.EqualFold(addr, "off") {
		addr = ""
	}
	if addr == "" {
		addr = strings.TrimSpace(os.Getenv("REDIS_ADDRESS"))
	}
	var rdb *redis.Client
	if addr != "" {
		candidate := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		})
		// Redis remains mandatory for the legacy Ops worker, but IndexWorker has
		// a MySQL lease/CAS correctness path and can degrade without it.
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := candidate.Ping(pingCtx).Err(); err == nil {
			rdb = candidate
		} else {
			g.Log().Warningf(ctx, "Redis unavailable: OpsWorker disabled; IndexWorker will use MySQL lease only: %v", err)
		}
		cancel()
	}
	if rdb != nil {
		opsWorker = task.NewOpsWorker(opsTaskRepo, rdb, opsAgent)
	}
	if ragIndexingEnabled {
		if executor, ok := ragService.(domain.IndexTaskExecutor); ok {
			indexWorker = task.NewIndexWorker(taskRepo, rdb, executor)
		}
	}

	return &App{
		Milvus:             milvusClient,
		RAG:                ragService,
		Documents:          docRepo,
		Storage:            store,
		Memory:             sessionService,
		ModelRouter:        modelRouter,
		Toolkit:            toolGateway,
		ChatAgent:          chatAgent,
		OpsAgent:           opsAgent,
		IntentRouter:       intentRouter,
		SessionRepo:        sessionRepo,
		ChatTurnRepo:       chatTurnRepo,
		OpsTaskRepo:        opsTaskRepo,
		AlertEventRepo:     alertEventRepo,
		ApprovalRepo:       approvalRepo,
		TraceRepo:          traceRepo,
		FaultKnowledgeRepo: faultKnowledgeRepo,
		ToolCallRecordRepo: toolCallRecordRepo,
		FeedbackRepo:       feedbackRepo,
		OpsWorker:          opsWorker,
		IndexWorker:        indexWorker,
		VectorGCWorker:     vectorGCWorker,
		AlertEventReaper:   alertEventReaper,
		VectorGCRepo:       vectorGCRepo,
		MCPTimeClient:      mcpTimeClient,
	}, nil
}

func pingMySQL(ctx context.Context) error {
	_, err := g.DB().Exec(ctx, "SELECT 1")
	return err
}

func pingRedis(ctx context.Context) error {
	_, err := g.Redis().Do(ctx, "PING")
	return err
}

// Close releases optional external clients.
func (a *App) Close() error {
	if a == nil || a.MCPTimeClient == nil {
		return nil
	}
	return a.MCPTimeClient.Close()
}

// Ready checks whether core dependencies are reachable.
func (a *App) Ready(ctx context.Context) map[string]string {
	// Readiness is called by load balancers frequently. Share one bounded
	// deadline across dependency checks so an unreachable optional datastore
	// cannot pin a handler goroutine or delay a rollout indefinitely.
	probeCtx, cancel := context.WithTimeout(ctx, readinessProbeTimeout)
	defer cancel()
	status := map[string]string{
		"mysql":              componentStatus(pingMySQL(probeCtx)),
		"redis":              componentStatus(pingRedis(probeCtx)),
		"milvus":             "skipped",
		"prometheus":         dataSourceStatus(probeCtx, "prometheus.base_url", "PROMETHEUS_URL"),
		"logs":               dataSourceStatus(probeCtx, "mcp.log.url", "MCP_LOG_URL"),
		"deployments":        dataSourceStatus(probeCtx, "deployment.base_url", "DEPLOYMENT_BASE_URL"),
		"ops_worker":         workerStatus(a.OpsWorker != nil && a.OpsWorker.Ready()),
		"index_worker":       workerStatus(a.IndexWorker != nil && a.IndexWorker.Ready()),
		"vector_gc_worker":   workerStatus(a.VectorGCWorker != nil && a.VectorGCWorker.Ready()),
		"alert_event_reaper": workerStatus(a.AlertEventReaper != nil && a.AlertEventReaper.Ready()),
		"chat_model":         modelStatus(probeCtx, a.ModelRouter, domain.ModelProfileChatFast),
		"ops_model":          modelStatus(probeCtx, a.ModelRouter, domain.ModelProfileOpsExec),
	}
	if a.Milvus != nil {
		status["milvus"] = componentStatus(a.Milvus.Ping(probeCtx))
	} else {
		status["milvus"] = "down"
	}
	if a.RAG == nil {
		status["rag"] = "down"
	} else if ragService, ok := a.RAG.(*rag.Service); ok && !ragService.Ready() {
		status["rag"] = "degraded"
	} else {
		status["rag"] = "up"
	}
	return status
}

// RefreshRAGInventory updates only aggregate logical inventory gauges. It is
// called by the metrics endpoint on scrape and deliberately does not treat
// database task history as current physical vector inventory.
func (a *App) RefreshRAGInventory(ctx context.Context) {
	if a == nil || a.Documents == nil {
		return
	}
	inventory, err := a.Documents.ActiveRAGInventory(ctx)
	if err != nil {
		observability.ObserveRAGInventoryRefreshError("logical")
		return
	}
	observability.SetRAGInventory(inventory.ActiveDocuments, inventory.ActivePublishedChunks, inventory.ActiveLegacyDocuments)
	if a.Milvus == nil {
		return
	}
	count, err := a.Milvus.PhysicalVectorCount(ctx)
	if err != nil {
		observability.ObserveRAGInventoryRefreshError("physical")
		return
	}
	observability.SetRAGPhysicalVectors(count)
}

func dataSourceStatus(ctx context.Context, yamlKey, envKey string) string {
	value := strings.TrimSpace(configx.String(ctx, yamlKey, envKey))
	if value == "" {
		return "skipped"
	}
	if strings.EqualFold(value, "local_mock") {
		return "up"
	}
	return "configured"
}

func workerStatus(ready bool) string {
	if ready {
		return "up"
	}
	return "down"
}

func modelStatus(ctx context.Context, router domain.ModelRouter, profile domain.ModelProfile) string {
	if router == nil {
		return "down"
	}
	if _, err := router.ChatModel(ctx, profile); err != nil {
		return "down"
	}
	return "up"
}

func componentStatus(err error) string {
	if err == nil {
		return "up"
	}
	return "down"
}

// MustInit panics when bootstrap fails in strict mode.
func MustInit() *App {
	ctx := gctx.New()
	app, err := Init(ctx)
	if err != nil {
		panic(fmt.Sprintf("bootstrap init failed: %v", err))
	}
	return app
}

// RedisClient returns the default Redis client.
func RedisClient() *gredis.Redis {
	return g.Redis()
}
