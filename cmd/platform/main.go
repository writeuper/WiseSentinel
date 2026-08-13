package main

import (
	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/gateway"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/gsession"
)

func main() {
	ctx := gctx.New()

	app, err := bootstrap.Init(ctx)
	if err != nil {
		g.Log().Fatal(ctx, "bootstrap failed:", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			g.Log().Warning(ctx, "close MCP client failed:", err)
		}
	}()
	if app.Milvus != nil {
		defer app.Milvus.Close()
	}

	// Start async workers.
	if app.OpsWorker != nil {
		app.OpsWorker.Start(ctx)
		g.Log().Info(ctx, "OpsWorker started")
	}
	if app.IndexWorker != nil {
		app.IndexWorker.Start(ctx)
		g.Log().Info(ctx, "IndexWorker started")
	}
	if app.VectorGCWorker != nil {
		app.VectorGCWorker.Start(ctx)
		g.Log().Info(ctx, "VectorGCWorker started")
	}
	if app.AlertEventReaper != nil {
		app.AlertEventReaper.Start(ctx)
		g.Log().Info(ctx, "AlertEventReaper started")
	}

	s := g.Server()
	s.SetSessionStorage(gsession.NewStorageMemory())
	gateway.Register(s, app)
	s.Run()
	g.Log().Info(ctx, "server stopped")
}
