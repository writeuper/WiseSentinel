package main

import (
	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/gateway"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

func main() {
	ctx := gctx.New()

	app, err := bootstrap.Init(ctx)
	if err != nil {
		g.Log().Fatal(ctx, "bootstrap failed:", err)
	}
	if app.Milvus != nil {
		defer app.Milvus.Close()
	}

	s := g.Server()
	gateway.Register(s, app)
	s.Run()
}
