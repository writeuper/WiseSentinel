package toolkit_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/toolkit"
)

func TestMain(m *testing.M) {
	// Set config path so GoFrame can find config.yaml from the project root.
	os.Setenv("GF_GCFG_PATH", "../../manifest/config")
	os.Exit(m.Run())
}

// ctxWithRoles returns a context with the given roles set.
func ctxWithRoles(ctx context.Context, roles ...string) context.Context {
	ctx = ctxkeys.WithTenantID(ctx, "default")
	ctx = ctxkeys.WithUserID(ctx, "test_user")
	ctx = ctxkeys.WithRoles(ctx, roles)
	ctx = ctxkeys.WithTraceID(ctx, "test_trace")
	return ctx
}

func TestNewGateway(t *testing.T) {
	ctx := context.Background()
	gw := toolkit.NewGateway(ctx)
	if gw == nil {
		t.Fatal("NewGateway returned nil")
	}
}

func TestListTools(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	gw := toolkit.NewGateway(ctx)

	tools, err := gw.ListTools(ctx, "default", domain.AgentTypeChat)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	t.Logf("Chat tools: %d", len(tools))
}

func TestGatewayAgentFilter(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "operator")
	gw := toolkit.NewGateway(ctx)

	chatTools, _ := gw.ListTools(ctx, "default", domain.AgentTypeChat)
	opsTools, _ := gw.ListTools(ctx, "default", domain.AgentTypeOps)

	t.Logf("Chat tools: %d, Ops tools: %d", len(chatTools), len(opsTools))
}

func TestGatewayInvokeUnknownTool(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "operator")
	gw := toolkit.NewGateway(ctx)

	_, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "nonexistent_tool",
		Input:    json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestGatewayInvokeGetCurrentTime(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "operator")
	gw := toolkit.NewGateway(ctx)

	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "get_current_time",
		Input:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Invoke get_current_time failed: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("expected status 'success', got %q", resp.Status)
	}
	if resp.Output == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestGatewayInvokeL0ForViewer(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	gw := toolkit.NewGateway(ctx)

	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "get_current_time",
		Input:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("viewer should be able to invoke L0 tool: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected success, got %q", resp.Status)
	}
}

func TestGatewayInvokeInternalDocsWithNoRAG(t *testing.T) {
	// query_internal_docs has no RAG service wired in test, so it should
	// still return success (will just have no results).
	ctx := ctxWithRoles(context.Background(), "operator")
	gw := toolkit.NewGateway(ctx)

	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "query_internal_docs",
		Input:    json.RawMessage(`{"query":"test"}`),
	})
	if err != nil {
		t.Fatalf("Invoke query_internal_docs failed: %v", err)
	}
	if resp.Status == "" {
		t.Fatal("expected non-empty status")
	}
	t.Logf("query_internal_docs result: status=%s, output=%s", resp.Status, resp.Output)
}
