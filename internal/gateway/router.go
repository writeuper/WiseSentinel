package gateway

import (
	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/gateway/handler"
	"wisesentinel-platform/internal/gateway/metrics"
	"wisesentinel-platform/internal/gateway/middleware"

	"github.com/gogf/gf/v2/net/ghttp"
)

// Register wires HTTP routes and middleware for the platform API.
func Register(s *ghttp.Server, app *bootstrap.App) {
	metrics.Register(s, app)
	s.Use(metrics.HTTPMetrics)
	handler.RegisterHealth(s, app)

	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(
			middleware.Recovery,
			middleware.Trace,
			middleware.CORS,
			middleware.TenantResolver,
			middleware.Auth,
			middleware.RBAC,
			middleware.RateLimit,
			middleware.RequestLogger,
			middleware.Audit,
			middleware.UnifiedResponse,
		)

		group.Bind(handler.NewV1(app))
	})

	s.Group("/internal/webhooks", func(group *ghttp.RouterGroup) {
		group.Middleware(
			middleware.Recovery,
			middleware.Trace,
			middleware.WebhookTenant,
			middleware.AlertmanagerSignature,
			middleware.RateLimit,
			middleware.RequestLogger,
			middleware.UnifiedResponse,
		)
		group.Bind(handler.NewV1(app))
	})

	s.Group("/api/v1", func(group *ghttp.RouterGroup) {
		group.Middleware(
			middleware.Recovery,
			middleware.Trace,
			middleware.CORS,
			middleware.TenantResolver,
			middleware.Auth,
			middleware.RBAC,
			middleware.RateLimit,
			middleware.RequestLogger,
			middleware.Audit,
			middleware.UnifiedResponse,
		)

		group.Bind(handler.NewV1(app))
	})
}
