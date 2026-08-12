package adapters

import (
	"context"
	"net/http"
	"os"
	"strings"

	"wisesentinel-platform/internal/pkg/configx"
)

func prometheusConfig(ctx context.Context) (string, string) {
	baseURL := configx.String(ctx, "prometheus.base_url", "PROMETHEUS_URL")
	bearerToken := strings.TrimSpace(os.Getenv("PROMETHEUS_BEARER_TOKEN"))
	return baseURL, bearerToken
}

func setPrometheusAuth(req *http.Request, bearerToken string) {
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
}
