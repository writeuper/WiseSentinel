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
