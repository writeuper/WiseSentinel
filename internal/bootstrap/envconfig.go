package bootstrap

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
)

// dotEnvPaths lists candidate .env file locations to search.
var dotEnvPaths = []string{".env"}

// loadDotEnv reads a .env file and sets each KEY=VALUE as an environment variable.
// Existing env vars are NOT overwritten.
func loadDotEnv() error {
	var path string
	for _, candidate := range dotEnvPaths {
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}
	if path == "" {
		return nil // .env file not found, skip
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open .env: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding single/double quotes if present
		if len(val) > 1 && (val[0] == '"' || val[0] == '\'') && val[0] == val[len(val)-1] {
			val = val[1 : len(val)-1]
		}
		if key == "" {
			continue
		}
		// Only set if not already present in the environment
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

// applyConfigFromEnv applies infrastructure overrides from environment / .env file.
// Must be called before any client init.
func applyConfigFromEnv(ctx context.Context) {
	_ = loadDotEnv()

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

	if apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY")); apiKey != "" {
		g.Log().Infof(ctx, "LLM_API_KEY loaded from environment")
	}
	if embedKey := strings.TrimSpace(os.Getenv("EMBED_API_KEY")); embedKey != "" {
		g.Log().Infof(ctx, "EMBED_API_KEY loaded from environment")
	}

	// Warm config adapter so yaml is loaded before handlers read secrets via configx.
	_ = g.Cfg().MustGet(ctx, "auth.jwt_secret")
}
