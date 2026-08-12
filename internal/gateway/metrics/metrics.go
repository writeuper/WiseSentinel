package metrics

import (
	"context"
	"time"

	"wisesentinel-platform/internal/observability"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var registry = observability.Registry

// RAGInventoryRefresher is deliberately a narrow dependency inversion point:
// repository code already reports other metrics, so this package must not
// import bootstrap and create a repository → metrics → bootstrap cycle.
type RAGInventoryRefresher interface {
	RefreshRAGInventory(context.Context)
}

// Register mounts the Prometheus /metrics endpoint.
func Register(s *ghttp.Server, app RAGInventoryRefresher) {
	s.BindHandler("/metrics", func(r *ghttp.Request) {
		if app != nil {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			app.RefreshRAGInventory(ctx)
			cancel()
		}
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(r.Response.Writer, r.Request)
	})
}
