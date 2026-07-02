package middleware

import (
	"strings"
	"time"

	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// Auth validates JWT Bearer token or development API key.
func Auth(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	ctx := r.Context()
	token := extractBearerToken(r)
	if token == "" {
		apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
		devKey := configx.String(ctx, "auth.dev_api_key", "DEV_API_KEY")
		if apiKey != "" && devKey != "" && apiKey == devKey {
			ctx = ctxkeys.WithUserID(ctx, "dev_api_user")
			ctx = ctxkeys.WithRoles(ctx, []string{"operator"})
			r.SetCtx(ctx)
			r.Middleware.Next()
			return
		}
		writeError(r, apperr.ErrUnauthorized)
		return
	}

	claims, err := auth.ParseToken(ctx, token)
	if err != nil {
		writeError(r, apperr.ErrUnauthorized)
		return
	}

	ctx = ctxkeys.WithUserID(ctx, claims.Subject)
	ctx = ctxkeys.WithRoles(ctx, claims.Roles)
	if claims.TenantID != "" {
		ctx = ctxkeys.WithTenantID(ctx, claims.TenantID)
	}
	r.SetCtx(ctx)
	r.Middleware.Next()
}

func extractBearerToken(r *ghttp.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return header
}

func isPublicPath(path string) bool {
	public := []string{
		"/health/live",
		"/health/ready",
		"/metrics",
		"/api/v1/auth/token",
	}
	for _, p := range public {
		if path == p {
			return true
		}
	}
	return false
}

// RBAC enforces role-based access for protected routes.
func RBAC(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	required := requiredRoles(r.URL.Path, r.Method)
	if len(required) == 0 {
		r.Middleware.Next()
		return
	}

	roles := ctxkeys.RolesFrom(r.Context())
	if hasAnyRole(roles, required) {
		r.Middleware.Next()
		return
	}
	writeError(r, apperr.ErrForbidden)
}

func requiredRoles(path, method string) []string {
	switch {
	case strings.HasPrefix(path, "/api/v1/admin"):
		return []string{"sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/ops"):
		return []string{"operator", "sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodDelete:
		return []string{"sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodPost:
		return []string{"operator", "sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/approvals"):
		return []string{"sre_admin", "platform_admin"}
	default:
		return nil
	}
}

const (
	httpMethodDelete = "DELETE"
	httpMethodPost   = "POST"
	httpMethodGet    = "GET"
)

func hasAnyRole(userRoles, required []string) bool {
	set := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		set[role] = struct{}{}
	}
	for _, role := range required {
		if _, ok := set[role]; ok {
			return true
		}
	}
	return false
}

// RateLimit applies per-user request throttling using Redis when available.
func RateLimit(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	ctx := r.Context()
	userID := ctxkeys.UserIDFrom(ctx)
	if userID == "" {
		userID = "anonymous"
	}

	limitKey := rateLimitKey(r.URL.Path)
	if limitKey == "" {
		r.Middleware.Next()
		return
	}

	maxPerMinute := g.Cfg().MustGet(ctx, limitKey, 60).Int()
	redisKey := "ws:" + ctxkeys.TenantIDFrom(ctx) + ":ratelimit:" + userID + ":" + limitKey

	count, err := g.Redis().Do(ctx, "INCR", redisKey)
	if err != nil {
		r.Middleware.Next()
		return
	}
	if count.Int() == 1 {
		_, _ = g.Redis().Do(ctx, "EXPIRE", redisKey, 60)
	}
	if count.Int() > maxPerMinute {
		writeError(r, apperr.ErrRateLimited)
		return
	}
	r.Middleware.Next()
}

func rateLimitKey(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/chat"):
		return "rate_limit.chat_per_minute"
	case strings.HasPrefix(path, "/api/v1/ops"):
		return "rate_limit.ops_per_minute"
	default:
		return ""
	}
}

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
