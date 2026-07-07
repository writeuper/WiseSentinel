package middleware

import (
	"strings"

	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/trace"

	"github.com/gogf/gf/v2/net/ghttp"
)

// Trace injects or propagates X-Trace-ID.
func Trace(r *ghttp.Request) {
	traceID := strings.TrimSpace(r.Header.Get("X-Trace-ID"))
	if traceID == "" {
		traceID = trace.NewID()
	}
	ctx := ctxkeys.WithTraceID(r.Context(), traceID)
	r.SetCtx(ctx)
	r.Response.Header().Set("X-Trace-ID", traceID)
	r.Middleware.Next()
}