package middleware

import (
	"time"

	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// RequestLogger logs basic request metadata.
func RequestLogger(r *ghttp.Request) {
	start := time.Now()
	r.Middleware.Next()
	g.Log().Infof(
		r.Context(),
		"%s %s status=%d latency=%s trace=%s tenant=%s user=%s",
		r.Method,
		r.URL.Path,
		r.Response.Status,
		time.Since(start),
		ctxkeys.TraceIDFrom(r.Context()),
		ctxkeys.TenantIDFrom(r.Context()),
		ctxkeys.UserIDFrom(r.Context()),
	)
}