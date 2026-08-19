package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestIsPublicPathDoesNotExposeCurrentIdentity(t *testing.T) {
	if isPublicPath("/api/v1/me") {
		t.Fatal("/api/v1/me must require authentication")
	}
}

func TestIsPublicPathAllowsOnlyExplicitUnauthenticatedEndpoints(t *testing.T) {
	for _, path := range []string{"/health/live", "/health/ready", "/metrics", "/auth/token", "/api/v1/auth/token"} {
		if !isPublicPath(path) {
			t.Fatalf("expected %s to be public", path)
		}
	}
}

func TestDevelopmentAPIKeyIdentityPinsTenant(t *testing.T) {
	ctx := ctxkeys.WithTenantID(context.Background(), "attacker-selected-tenant")
	ctx = withDevelopmentAPIKeyIdentity(ctx)
	if got := ctxkeys.TenantIDFrom(ctx); got != domain.DefaultTenantID {
		t.Fatalf("development API key tenant = %q, want %q", got, domain.DefaultTenantID)
	}
	if got := ctxkeys.UserIDFrom(ctx); got != "dev_api_user" {
		t.Fatalf("development API key user = %q", got)
	}
}

func TestDevelopmentAPIKeyDisabledInProduction(t *testing.T) {
	old := os.Getenv("APP_ENV")
	t.Cleanup(func() { _ = os.Setenv("APP_ENV", old) })
	_ = os.Setenv("APP_ENV", "production")
	if developmentAPIKeyEnabled() {
		t.Fatal("development API key must be disabled in production")
	}
	_ = os.Setenv("APP_ENV", "staging")
	if !developmentAPIKeyEnabled() {
		t.Fatal("development API key should be enabled outside production")
	}
}

func TestServiceAPIKeyBindsConfiguredIdentityAndIgnoresRequestTenant(t *testing.T) {
	for key, value := range map[string]string{
		"SERVICE_API_KEY":       "service-key-canary",
		"SERVICE_API_TENANT_ID": "tenant-service",
		"SERVICE_API_USER_ID":   "svc-ops",
		"SERVICE_API_ROLES":     "operator,sre_admin",
		"SERVICE_API_SCOPES":    "ops:execute,trace:read",
	} {
		old := os.Getenv(key)
		t.Cleanup(func() { _ = os.Setenv(key, old) })
		_ = os.Setenv(key, value)
	}
	ctx := ctxkeys.WithTenantID(context.Background(), "attacker-tenant")
	got, ok := withServiceAPIKeyIdentity(ctx, "service-key-canary")
	if !ok {
		t.Fatal("configured service API key rejected")
	}
	if gotTenant := ctxkeys.TenantIDFrom(got); gotTenant != "tenant-service" {
		t.Fatalf("service tenant = %q, want tenant-service", gotTenant)
	}
	if gotUser := ctxkeys.UserIDFrom(got); gotUser != "svc-ops" {
		t.Fatalf("service user = %q, want svc-ops", gotUser)
	}
	if roles := ctxkeys.RolesFrom(got); len(roles) != 2 || roles[0] != "operator" || roles[1] != "sre_admin" {
		t.Fatalf("service roles = %#v", roles)
	}
	if scopes := ctxkeys.ScopesFrom(got); len(scopes) != 2 || scopes[0] != "ops:execute" || scopes[1] != "trace:read" {
		t.Fatalf("service scopes = %#v", scopes)
	}
}

func TestServiceAPIKeyRejectsWrongOrMissingConfiguration(t *testing.T) {
	old := os.Getenv("SERVICE_API_KEY")
	t.Cleanup(func() { _ = os.Setenv("SERVICE_API_KEY", old) })
	_ = os.Setenv("SERVICE_API_KEY", "service-key")
	if _, ok := withServiceAPIKeyIdentity(context.Background(), "wrong-key"); ok {
		t.Fatal("wrong service API key accepted")
	}
	if _, ok := withServiceAPIKeyIdentity(context.Background(), ""); ok {
		t.Fatal("empty service API key accepted")
	}
}

func TestServiceAPIKeyAcceptsPrimaryAndPreviousSHA256DuringRotation(t *testing.T) {
	t.Setenv("SERVICE_API_KEY", "")
	t.Setenv("SERVICE_API_KEY_SHA256", hashServiceKey("current-key"))
	t.Setenv("SERVICE_API_KEY_PREVIOUS_SHA256", hashServiceKey("previous-key"))
	if !serviceAPIKeyMatches(context.Background(), "current-key") {
		t.Fatal("primary SHA-256 service key rejected")
	}
	if !serviceAPIKeyMatches(context.Background(), "previous-key") {
		t.Fatal("previous SHA-256 service key rejected during rotation")
	}
	if serviceAPIKeyMatches(context.Background(), "wrong-key") {
		t.Fatal("wrong SHA-256 service key accepted")
	}
}

func TestServiceAPIKeyRegistryParsesBoundedIdentityLists(t *testing.T) {
	roles, ok := decodeIdentityList(`["operator","sre_admin"]`, 32, 64)
	if !ok || len(roles) != 2 || roles[1] != "sre_admin" {
		t.Fatalf("roles = %#v, ok=%t", roles, ok)
	}
	if _, ok := decodeIdentityList(`["operator","operator"]`, 32, 64); ok {
		t.Fatal("duplicate roles accepted")
	}
	if _, ok := decodeIdentityList(`{"role":"operator"}`, 32, 64); ok {
		t.Fatal("non-list identity accepted")
	}
}

func TestServiceAPIKeyRegistryIsOptIn(t *testing.T) {
	t.Setenv("SERVICE_API_KEY_REGISTRY_ENABLED", "false")
	if serviceAPIKeyRegistryEnabled(context.Background()) {
		t.Fatal("service API Key registry enabled without explicit opt-in")
	}
	_, enabled, ok := serviceAPIKeyRegistryIdentity(context.Background(), "key")
	if enabled || ok {
		t.Fatal("disabled registry performed an authentication lookup")
	}
}

func TestServiceAPIKeyRejectsMalformedSHA256Configuration(t *testing.T) {
	t.Setenv("SERVICE_API_KEY", "")
	t.Setenv("SERVICE_API_KEY_SHA256", "not-a-digest")
	t.Setenv("SERVICE_API_KEY_PREVIOUS_SHA256", "00")
	if serviceAPIKeyMatches(context.Background(), "current-key") {
		t.Fatal("malformed SHA-256 configuration accepted")
	}
}

func hashServiceKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func TestJWTIdentityUsesClaimTenantInsteadOfRequestTenant(t *testing.T) {
	ctx := ctxkeys.WithTenantID(context.Background(), "attacker-selected-tenant")
	ctx, ok := withJWTIdentity(ctx, &auth.Claims{TenantID: "tenant-from-jwt"})
	if !ok {
		t.Fatal("valid tenant claim rejected")
	}
	if got := ctxkeys.TenantIDFrom(ctx); got != "tenant-from-jwt" {
		t.Fatalf("tenant = %q, want JWT claim tenant", got)
	}
}

func TestJWTIdentityRejectsMissingTenantClaim(t *testing.T) {
	ctx := ctxkeys.WithTenantID(context.Background(), "attacker-selected-tenant")
	if _, ok := withJWTIdentity(ctx, &auth.Claims{}); ok {
		t.Fatal("JWT without tenant claim must be rejected")
	}
}
