package bootstrap

import (
	"context"
	"os"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
)

// applyConfigFromEnv applies infrastructure overrides that must be wired before client init.
func applyConfigFromEnv(ctx context.Context) {
	if dsn := strings.TrimSpace(os.Getenv("MYSQL_DSN")); dsn != "" {
		gdb.SetConfigGroup("default", gdb.ConfigGroup{
			gdb.ConfigNode{Link: dsn},
		})
	}

	if addr := strings.TrimSpace(os.Getenv("REDIS_ADDRESS")); addr != "" {
		gredis.SetConfig(&gredis.Config{
			Address: addr,
			Pass:    strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		})
	}

	// Warm config adapter so yaml is loaded before handlers read secrets via configx.
	_ = g.Cfg().MustGet(ctx, "auth.jwt_secret")
}
