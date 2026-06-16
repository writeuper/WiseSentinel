package bootstrap

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/rag/client"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

// App holds shared infrastructure clients initialized at startup.
type App struct {
	Milvus *client.MilvusClient
}

// Init wires database, cache, and vector store clients.
func Init(ctx context.Context) (*App, error) {
	if err := pingMySQL(ctx); err != nil {
		g.Log().Warning(ctx, "MySQL not ready:", err)
	}
	if err := pingRedis(ctx); err != nil {
		g.Log().Warning(ctx, "Redis not ready:", err)
	}

	milvusClient, err := client.NewMilvusClient(ctx)
	if err != nil {
		g.Log().Warning(ctx, "Milvus not ready:", err)
	}

	return &App{Milvus: milvusClient}, nil
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
