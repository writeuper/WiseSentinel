package middleware

import (
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// Audit records request metadata after handler execution.
func Audit(r *ghttp.Request) {
	start := time.Now()
	r.Middleware.Next()

	if isPublicPath(r.URL.Path) {
		return
	}

	action := auditAction(r.URL.Path, r.Method)
	if action == "" {
		return
	}

	_, err := g.DB().Insert(r.Context(), "ws_audit_log", g.Map{
		"tenant_id":     ctxkeys.TenantIDFrom(r.Context()),
		"trace_id":      ctxkeys.TraceIDFrom(r.Context()),
		"user_id":       ctxkeys.UserIDFrom(r.Context()),
		"action":        action,
		"resource_type": "http",
		"resource_id":   r.URL.Path,
		"response_code": r.Response.Status,
		"latency_ms":    time.Since(start).Milliseconds(),
	})
	if err != nil {
		g.Log().Warningf(r.Context(), "audit log insert failed: %v", err)
	}
}

func auditAction(path, method string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/chat"):
		return "chat.invoke"
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodPost:
		return "doc.upload"
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodDelete:
		return "doc.delete"
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodGet:
		return "doc.read"
	case strings.HasPrefix(path, "/api/v1/ops"):
		return "ops.analyze"
	default:
		return ""
	}
}