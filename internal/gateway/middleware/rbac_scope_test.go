package middleware

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestServiceScopesAreRequiredForHighRiskRoutes(t *testing.T) {
	tests := []struct {
		path, method, want string
	}{
		{"/api/v1/ops/analyze", httpMethodPost, "ops:execute"},
		{"/api/v1/ops/tasks", httpMethodGet, "ops:read"},
		{"/api/v1/admin/agent-configs/v2/activate", httpMethodPut, "agent_config:write"},
		{"/api/v1/knowledge/documents", httpMethodPost, "knowledge:write"},
		{"/api/v1/approvals/a/decision", httpMethodPost, "approval:decide"},
		{"/api/v1/traces/t-1", httpMethodGet, "trace:read"},
	}
	for _, tc := range tests {
		if got := requiredScope(tc.path, tc.method); got != tc.want {
			t.Fatalf("scope %s %s = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
	ctx := ctxkeys.WithScopes(ctxkeys.WithAuthMethod(context.Background(), "service_api_key"), []string{"ops:execute"})
	if !hasScope(ctxkeys.ScopesFrom(ctx), "ops:execute") || hasScope(ctxkeys.ScopesFrom(ctx), "ops:read") {
		t.Fatal("scope matching is not exact")
	}
}

func TestJWTUsersDoNotNeedServiceScopeContext(t *testing.T) {
	if got := requiredScope("/api/v1/ops/analyze", httpMethodPost); got == "" {
		t.Fatal("route lost scope policy")
	}
	// Scope enforcement is conditional on AuthMethod=service_api_key; JWT
	// users remain governed by the existing role matrix.
	ctx := ctxkeys.WithAuthMethod(context.Background(), "jwt")
	if ctxkeys.AuthMethodFrom(ctx) == "service_api_key" {
		t.Fatal("JWT context mislabeled as service API key")
	}
}
