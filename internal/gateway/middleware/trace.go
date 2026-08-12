package middleware

import (
	"strings"

	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/trace"

	"github.com/gogf/gf/v2/net/ghttp"
)

const maxTraceIDLength = 128

// Trace injects or propagates X-Trace-ID.
func Trace(r *ghttp.Request) {
	traceID := strings.TrimSpace(r.Header.Get("X-Trace-ID"))
	if !isValidTraceID(traceID) {
		traceID = trace.NewID()
	}
	ctx := ctxkeys.WithTraceID(r.Context(), traceID)
	r.SetCtx(ctx)
	r.Response.Header().Set("X-Trace-ID", traceID)
	r.Middleware.Next()
}

// isValidTraceID restricts client-provided correlation identifiers to a small,
// log-safe alphabet. Trace IDs are copied into audit records and logs; tenant,
// query and arbitrary header text must never become trace identifiers.
func isValidTraceID(value string) bool {
	if value == "" || len(value) > maxTraceIDLength {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
