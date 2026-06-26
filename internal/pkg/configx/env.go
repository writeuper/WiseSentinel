package configx

import (
	"context"
	"os"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
)

// String returns the environment variable when set, otherwise the yaml config value.
func String(ctx context.Context, yamlKey, envKey string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	return g.Cfg().MustGet(ctx, yamlKey).String()
}
