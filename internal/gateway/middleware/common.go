package middleware

import (
	"net/http"
	"strings"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/response"
	"wisesentinel-platform/internal/pkg/trace"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// Recovery catches panics and returns a unified error response.
func Recovery(r *ghttp.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			g.Log().Errorf(r.Context(), "panic recovered: %v", rec)
			writeError(r, apperr.ErrInternal)
		}
	}()
	r.Middleware.Next()
}

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

// CORS handles cross-origin requests for the portal.
func CORS(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

// TenantResolver resolves tenant from header with default fallback.
func TenantResolver(r *ghttp.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = g.Cfg().MustGet(r.Context(), "tenant.default_id", domain.DefaultTenantID).String()
	}
	ctx := ctxkeys.WithTenantID(r.Context(), tenantID)
	r.SetCtx(ctx)
	r.Middleware.Next()
}

// UnifiedResponse wraps handler output in the standard envelope.
func UnifiedResponse(r *ghttp.Request) {
	r.Middleware.Next()

	if r.Response.BufferLength() > 0 {
		return
	}

	path := r.URL.Path
	if strings.HasPrefix(path, "/health") || path == "/metrics" {
		return
	}

	err := r.GetError()
	if err != nil {
		writeHandlerError(r, err)
		return
	}

	res := r.GetHandlerResponse()
	r.Response.WriteJson(response.OK(res))
}

func writeHandlerError(r *ghttp.Request, err error) {
	if ae, ok := err.(*apperr.AppError); ok {
		r.Response.Status = ae.HTTP
		r.Response.WriteJson(response.Fail(ae.Code, ae.Message))
		return
	}
	r.Response.Status = http.StatusInternalServerError
	r.Response.WriteJson(response.Fail(apperr.ErrInternal.Code, err.Error()))
}

func writeError(r *ghttp.Request, ae *apperr.AppError) {
	r.Response.Status = ae.HTTP
	r.Response.WriteJson(response.Fail(ae.Code, ae.Message))
}

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
