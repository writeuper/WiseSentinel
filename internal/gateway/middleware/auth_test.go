package middleware

import (
	"context"
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
	for _, path := range []string{"/health/live", "/health/ready", "/metrics", "/api/v1/auth/token"} {
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
