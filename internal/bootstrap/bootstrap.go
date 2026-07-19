package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	chatagent "wisesentinel-platform/internal/agent/chat"
	"wisesentinel-platform/internal/agent/knowledge"
	opsagent "wisesentinel-platform/internal/agent/ops"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/memory"
	"wisesentinel-platform/internal/model"
	"wisesentinel-platform/internal/orchestrator/router"
	"wisesentinel-platform/internal/orchestrator/task"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/repository"
	"wisesentinel-platform/internal/toolkit"
	"wisesentinel-platform/internal/toolkit/adapters"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/redis/go-redis/v9"
)

// App holds shared infrastructure clients initialized at startup.
type App struct {
	Milvus       *client.MilvusClient
	RAG          domain.RAGService
	Documents    *repository.DocumentRepo
	Storage      *storage.LocalStore
	Memory       domain.SessionService
	ModelRouter  domain.ModelRouter
	Toolkit      domain.ToolGateway
	ChatAgent    domain.ChatAgent
	OpsAgent     domain.OpsAgent
	IntentRouter domain.IntentRouter
	SessionRepo  *repository.SessionRepo
	OpsTaskRepo  *repository.OpsTaskRepo
	ApprovalRepo *repository.ApprovalRepo
	TraceRepo    *repository.AgentTraceRepo
	OpsWorker    *task.OpsWorker
}

// Init wires database, cache, vector store, and all business services.
func Init(ctx context.Context) (*App, error) {
	applyConfigFromEnv(ctx)

	if err := pingMySQL(ctx); err != nil {
		g.Log().Warning(ctx, "MySQL not ready:", err)
	}
	if err := pingRedis(ctx); err != nil {
		g.Log().Warning(ctx, "Redis not ready:", err)
	}

	store := storage.NewLocalStore(ctx)
	docRepo := repository.NewDocumentRepo()
	taskRepo := repository.NewIndexTaskRepo()
	sessionRepo := repository.NewSessionRepo()
	opsTaskRepo := repository.NewOpsTaskRepo()

	// Model Router
	modelRouter := model.NewRouter(ctx)

	// Tool Gateway
	toolGateway := toolkit.NewGateway(ctx)

	// Approval Repo — wire into gateway for L2 tool approval flow
	approvalRepo := repository.NewApprovalRepo()
	traceRepo := repository.NewAgentTraceRepo()
	toolGateway.SetApprovalRepo(approvalRepo)

	milvusClient, err := client.NewMilvusClient(ctx)
	if err != nil {
		g.Log().Warning(ctx, "Milvus not ready:", err)
	}

	var ragService domain.RAGService
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
				// Wire RAG service into the query_internal_docs adapter
				adapters.SetRAGServiceForInternalDocs(ragService)
			}
		}
	}

	// Memory / Session Service
	sessionService := memory.NewRedisSessionStore(sessionRepo)

	// Intent Router
	intentRouter := router.NewRuleRouter()

	// Chat Agent
	chatAgent := chatagent.NewAgent(modelRouter, ragService, toolGateway)

	// Ops Agent (M4 — full Plan-Execute-Replan)
	opsAgent := opsagent.NewAgent(modelRouter, toolGateway, opsTaskRepo)

	// Ops Worker (M4 — DB polling with distributed lock)
	// Build a standalone *redis.Client from the same REDIS_ADDRESS env
	// so the worker can call distributed-lock primitives directly
	// without depending on GoFrame's adapter abstraction.
	var opsWorker *task.OpsWorker
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
	if addr != "" {
		rdb := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		})
		// Best-effort ping; if it fails the worker simply never starts.
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := rdb.Ping(pingCtx).Err(); err == nil {
			opsWorker = task.NewOpsWorker(opsTaskRepo, rdb, opsAgent)
		}
		cancel()
	}

	return &App{
		Milvus:       milvusClient,
		RAG:          ragService,
		Documents:    docRepo,
		Storage:      store,
		Memory:       sessionService,
		ModelRouter:  modelRouter,
		Toolkit:      toolGateway,
		ChatAgent:    chatAgent,
		OpsAgent:     opsAgent,
		IntentRouter: intentRouter,
		SessionRepo:  sessionRepo,
		OpsTaskRepo:  opsTaskRepo,
		ApprovalRepo: approvalRepo,
		TraceRepo:    traceRepo,
		OpsWorker:    opsWorker,
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

// Ready checks whether core dependencies are reachable.
func (a *App) Ready(ctx context.Context) map[string]string {
	status := map[string]string{
		"mysql":  componentStatus(pingMySQL(ctx)),
		"redis":  componentStatus(pingRedis(ctx)),
		"milvus": "skipped",
	}
	if a.Milvus != nil {
		status["milvus"] = componentStatus(a.Milvus.Ping(ctx))
	} else {
		status["milvus"] = "down"
	}
	return status
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
