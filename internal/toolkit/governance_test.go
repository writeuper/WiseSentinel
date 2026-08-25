package toolkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/toolkit"
)

func TestToolCallFingerprintCanonicalizesObjectKeysAndIsolatesTask(t *testing.T) {
	a := toolkit.ToolCallFingerprint("tenant-a", "task-a", "get_current_time", json.RawMessage(`{"b":2,"a":1}`), "prod")
	b := toolkit.ToolCallFingerprint("tenant-a", "task-a", "get_current_time", json.RawMessage(`{"a":1,"b":2}`), "prod")
	if a != b {
		t.Fatalf("same semantic JSON must hash identically: %s != %s", a, b)
	}
	if a == toolkit.ToolCallFingerprint("tenant-a", "task-b", "get_current_time", json.RawMessage(`{"a":1,"b":2}`), "prod") {
		t.Fatal("fingerprint must be task isolated")
	}
}

func TestGatewayRejectsDuplicateInSameTask(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	ctx = ctxkeys.WithTaskID(ctx, "task-duplicate")
	budget := domain.DefaultToolBudget(4)
	ctx = ctxkeys.WithToolBudget(ctx, &budget, &domain.ToolBudgetState{})
	gw := toolkit.NewGateway(ctx)
	req := &domain.ToolInvokeRequest{TenantID: "default", TaskID: "task-duplicate", ToolName: "get_current_time", AgentType: domain.AgentTypeChat, Input: json.RawMessage(`{"b":2,"a":1}`)}
	if _, err := gw.Invoke(ctx, req); err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	req.Input = json.RawMessage(`{"a":1,"b":2}`)
	_, err := gw.Invoke(ctx, req)
	if !errors.Is(err, apperr.ErrToolDuplicate) {
		t.Fatalf("duplicate error=%v", err)
	}
}

func TestGatewayEnforcesTaskToolBudget(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	ctx = ctxkeys.WithTaskID(ctx, "task-budget")
	budget := domain.ToolBudget{MaxToolCalls: 1, MaxSameToolCalls: 2}
	ctx = ctxkeys.WithToolBudget(ctx, &budget, &domain.ToolBudgetState{})
	gw := toolkit.NewGateway(ctx)
	if _, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{TenantID: "default", TaskID: "task-budget", ToolName: "get_current_time", AgentType: domain.AgentTypeChat, Input: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	_, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{TenantID: "default", TaskID: "task-budget", ToolName: "mcp_time_get_current_time", AgentType: domain.AgentTypeChat, Input: json.RawMessage(`{}`)})
	if !errors.Is(err, apperr.ErrToolBudgetExceeded) {
		t.Fatalf("budget error=%v", err)
	}
}

func TestGatewayRejectsAgentToolMismatch(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	gw := toolkit.NewGateway(ctx)
	_, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{ToolName: "search_logs", AgentType: domain.AgentTypeChat, Input: json.RawMessage(`{"query":"x"}`)})
	if !errors.Is(err, apperr.ErrAgentToolDenied) {
		t.Fatalf("mismatch error=%v", err)
	}
}

func TestGatewayValidatesConfiguredToolSchemaAndRange(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	gw := toolkit.NewGateway(ctx)
	_, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{ToolName: "mcp_time_convert_time", AgentType: domain.AgentTypeChat, Input: json.RawMessage(`{"time":"2026-08-23 09:00:00"}`)})
	if !errors.Is(err, apperr.ErrArgumentMissing) {
		t.Fatalf("missing argument error=%v", err)
	}
	ctx = ctxkeys.WithTaskID(ctx, "task-range")
	budget := domain.DefaultToolBudget(3)
	budget.MaxQueryTimeRangeMS = 60_000
	ctx = ctxkeys.WithToolBudget(ctx, &budget, &domain.ToolBudgetState{})
	_, err = gw.Invoke(ctx, &domain.ToolInvokeRequest{TenantID: "default", TaskID: "task-range", ToolName: "query_metric_range", AgentType: domain.AgentTypeOps, Input: json.RawMessage(`{"query":"up","start":"2026-08-20T00:00:00Z","end":"2026-08-23T00:00:00Z"}`)})
	if !errors.Is(err, apperr.ErrArgumentOutOfRange) {
		t.Fatalf("time-range error=%v", err)
	}
}
