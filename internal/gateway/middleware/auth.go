package middleware

import (
	"strings"

	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

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
		"/api/v1/me",
	}
	for _, p := range public {
		if path == p {
			return true
		}
	}
	return false
}