package middleware

import (
	"context"
	"os"
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/net/ghttp"
)

// Auth validates JWT Bearer token or development API key.
func Auth(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) || isWebhookPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	ctx := r.Context()
	token := extractBearerToken(r)
	if token == "" {
		apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
		devKey := configx.String(ctx, "auth.dev_api_key", "DEV_API_KEY")
		if apiKey != "" && devKey != "" && apiKey == devKey && developmentAPIKeyEnabled() {
			ctx = withDevelopmentAPIKeyIdentity(ctx)
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

	var valid bool
	ctx, valid = withJWTIdentity(ctx, claims)
	if !valid {
		writeError(r, apperr.ErrUnauthorized)
		return
	}
	r.SetCtx(ctx)
	r.Middleware.Next()
}

// withDevelopmentAPIKeyIdentity binds the development key to one fixed
// development tenant. Request headers must never choose an API key's tenant.
func withDevelopmentAPIKeyIdentity(ctx context.Context) context.Context {
	ctx = ctxkeys.WithUserID(ctx, "dev_api_user")
	ctx = ctxkeys.WithRoles(ctx, []string{"operator"})
	return ctxkeys.WithTenantID(ctx, domain.DefaultTenantID)
}

// withJWTIdentity accepts only self-contained tenant claims. Falling back to
// X-Tenant-ID would let a malformed or legacy JWT select its own tenant.
func withJWTIdentity(ctx context.Context, claims *auth.Claims) (context.Context, bool) {
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" {
		return ctx, false
	}
	ctx = ctxkeys.WithUserID(ctx, claims.Subject)
	ctx = ctxkeys.WithRoles(ctx, claims.Roles)
	return ctxkeys.WithTenantID(ctx, claims.TenantID), true
}

func developmentAPIKeyEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
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

func isWebhookPath(path string) bool {
	return path == "/internal/webhooks/alertmanager"
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
