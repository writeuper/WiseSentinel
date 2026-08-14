package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
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
		if serviceCtx, ok := withServiceAPIKeyIdentity(ctx, apiKey); ok {
			r.SetCtx(serviceCtx)
			r.Middleware.Next()
			return
		}
		devKey := configx.String(ctx, "auth.dev_api_key", "DEV_API_KEY")
		if apiKey != "" && equalSecret(apiKey, devKey) && developmentAPIKeyEnabled() {
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

// withServiceAPIKeyIdentity authenticates a non-development workload key and
// binds it to an explicitly configured service principal. The presented key
// never selects tenant, user or roles; those values come from deployment
// configuration. This is intentionally a single fixed principal until a
// database-backed key registry with rotation/revocation is introduced.
func withServiceAPIKeyIdentity(ctx context.Context, presented string) (context.Context, bool) {
	if !serviceAPIKeyMatches(ctx, presented) {
		return ctx, false
	}
	tenantID := strings.TrimSpace(os.Getenv("SERVICE_API_TENANT_ID"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(g.Cfg().MustGet(ctx, "auth.service_tenant_id", domain.DefaultTenantID).String())
	}
	userID := strings.TrimSpace(os.Getenv("SERVICE_API_USER_ID"))
	if userID == "" {
		userID = strings.TrimSpace(g.Cfg().MustGet(ctx, "auth.service_user_id", "service_api_user").String())
	}
	roles := configuredRoles(ctx)
	if tenantID == "" || userID == "" || len(roles) == 0 {
		return ctx, false
	}
	ctx = ctxkeys.WithTenantID(ctx, tenantID)
	ctx = ctxkeys.WithUserID(ctx, userID)
	ctx = ctxkeys.WithRoles(ctx, roles)
	ctx = ctxkeys.WithAuthMethod(ctx, "service_api_key")
	return ctxkeys.WithScopes(ctx, configuredScopes(ctx)), true
}

// serviceAPIKeyMatches accepts either a secret-manager supplied plaintext key
// or a SHA-256 digest. A previous digest can remain configured during a
// bounded rotation window; no key material is emitted or persisted.
func serviceAPIKeyMatches(ctx context.Context, presented string) bool {
	configured := strings.TrimSpace(os.Getenv("SERVICE_API_KEY"))
	if configured == "" {
		configured = configuredAuthValue(ctx, "auth.service_api_key", "SERVICE_API_KEY", "")
	}
	if configured != "" && equalSecret(presented, configured) {
		return true
	}
	for _, configuredHash := range configuredServiceKeyHashes(ctx) {
		if equalSHA256(presented, configuredHash) {
			return true
		}
	}
	return false
}

func configuredServiceKeyHashes(ctx context.Context) []string {
	values := []string{
		strings.TrimSpace(os.Getenv("SERVICE_API_KEY_SHA256")),
		strings.TrimSpace(os.Getenv("SERVICE_API_KEY_PREVIOUS_SHA256")),
	}
	if values[0] == "" {
		values[0] = configuredAuthValue(ctx, "auth.service_api_key_sha256", "SERVICE_API_KEY_SHA256", "")
	}
	if values[1] == "" {
		values[1] = configuredAuthValue(ctx, "auth.service_api_key_previous_sha256", "SERVICE_API_KEY_PREVIOUS_SHA256", "")
	}
	return values
}

func configuredAuthValue(ctx context.Context, yamlKey, envKey, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value
	}
	if value, err := g.Cfg().Get(ctx, yamlKey); err == nil && value != nil {
		if text := strings.TrimSpace(value.String()); text != "" {
			return text
		}
	}
	return fallback
}

func equalSHA256(presented, configuredHex string) bool {
	if presented == "" || len(configuredHex) != sha256.Size*2 {
		return false
	}
	configured, err := hex.DecodeString(configuredHex)
	if err != nil || len(configured) != sha256.Size {
		return false
	}
	digest := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(digest[:], configured) == 1
}

func configuredRoles(ctx context.Context) []string {
	value := strings.TrimSpace(os.Getenv("SERVICE_API_ROLES"))
	if value == "" {
		value = configuredAuthValue(ctx, "auth.service_roles", "SERVICE_API_ROLES", "operator")
	}
	parts := strings.Split(value, ",")
	roles := make([]string, 0, len(parts))
	for _, part := range parts {
		if role := strings.TrimSpace(part); role != "" {
			roles = append(roles, role)
		}
	}
	return roles
}

func configuredScopes(ctx context.Context) []string {
	value := strings.TrimSpace(os.Getenv("SERVICE_API_SCOPES"))
	if value == "" {
		value = configuredAuthValue(ctx, "auth.service_scopes", "SERVICE_API_SCOPES", "chat:invoke")
	}
	parts := strings.Split(value, ",")
	scopes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if scope := strings.TrimSpace(part); scope != "" {
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func equalSecret(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftBytes, rightBytes := []byte(left), []byte(right)
	if len(leftBytes) != len(rightBytes) {
		// Keep comparison work independent of the common prefix while avoiding
		// accepting keys of different lengths.
		padded := make([]byte, len(leftBytes))
		copy(padded, rightBytes)
		subtle.ConstantTimeCompare(leftBytes, padded)
		return false
	}
	return subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

// withDevelopmentAPIKeyIdentity binds the development key to one fixed
// development tenant. Request headers must never choose an API key's tenant.
func withDevelopmentAPIKeyIdentity(ctx context.Context) context.Context {
	ctx = ctxkeys.WithUserID(ctx, "dev_api_user")
	ctx = ctxkeys.WithRoles(ctx, []string{"operator"})
	ctx = ctxkeys.WithAuthMethod(ctx, "development_api_key")
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
	ctx = ctxkeys.WithAuthMethod(ctx, "jwt")
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
