package bootstrap

import (
	"context"
	"fmt"

	chatagent "wisesentinel-platform/internal/agent/chat"
	"wisesentinel-platform/internal/agent/knowledge"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/memory"
	"wisesentinel-platform/internal/model"
	"wisesentinel-platform/internal/orchestrator/router"
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
)

// App holds shared infrastructure clients initialized at startup.
type App struct {
	Milvus        *client.MilvusClient
	RAG           domain.RAGService
	Documents     *repository.DocumentRepo
	Storage       *storage.LocalStore
	Memory        domain.SessionService
	ModelRouter   domain.ModelRouter
	Toolkit       domain.ToolGateway
	ChatAgent     *chatagent.Agent
	OpsAgent      domain.AgentRunner
	IntentRouter  domain.IntentRouter
	SessionRepo   *repository.SessionRepo
	OpsTaskRepo   *repository.OpsTaskRepo
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

	// Ops Agent (M4 - placeholder for now, will be fully implemented in M4)
	opsAgent := NewOpsAgentPlaceholder(modelRouter, ragService, toolGateway, opsTaskRepo)

	return &App{
		Milvus:        milvusClient,
		RAG:           ragService,
		Documents:     docRepo,
		Storage:       store,
		Memory:        sessionService,
		ModelRouter:   modelRouter,
		Toolkit:       toolGateway,
		ChatAgent:     chatAgent,
		OpsAgent:      opsAgent,
		IntentRouter:  intentRouter,
		SessionRepo:   sessionRepo,
		OpsTaskRepo:   opsTaskRepo,
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