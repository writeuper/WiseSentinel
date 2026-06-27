package bootstrap

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/agent/knowledge"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

// App holds shared infrastructure clients initialized at startup.
type App struct {
	Milvus    *client.MilvusClient
	RAG       domain.RAGService
	Documents *repository.DocumentRepo
	Storage   *storage.LocalStore
}

// Init wires database, cache, and vector store clients.
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
			pipeline := knowledge.NewPipeline(store, idx)
			ragService = rag.NewService(pipeline, retr, idx, taskRepo)
		}
	}

	return &App{
		Milvus:    milvusClient,
		RAG:       ragService,
		Documents: docRepo,
		Storage:   store,
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
