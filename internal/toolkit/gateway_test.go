package toolkit_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/toolkit"
	mcpadapter "wisesentinel-platform/internal/toolkit/mcp"

	"github.com/cloudwego/eino/components/tool"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
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

func TestGatewayInvokeNilRequestReturnsBadRequest(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "operator")
	gw := toolkit.NewGateway(ctx)
	_, err := gw.Invoke(ctx, nil)
	if err == nil {
		t.Fatal("expected nil request to be rejected")
	}
	if !strings.Contains(err.Error(), "参数校验失败") {
		t.Fatalf("expected bad-request error, got %v", err)
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
	if resp.LatencyMS < 1 {
		t.Fatalf("executed tool latency must be at least 1ms, got %d", resp.LatencyMS)
	}
}

func TestGatewayInvokeMCPTimeConversionFallback(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	gw := toolkit.NewGateway(ctx)
	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "mcp_time_convert_time",
		Input:    json.RawMessage(`{"source_timezone":"Asia/Shanghai","time":"2026-08-13 09:00:00","target_timezone":"UTC"}`),
	})
	if err != nil {
		t.Fatalf("fallback conversion failed: %v", err)
	}
	if !strings.Contains(resp.Output, `"target_timezone":"UTC"`) || !strings.Contains(resp.Output, `"time":"2026-08-13 01:00:00"`) {
		t.Fatalf("unexpected conversion output: %s", resp.Output)
	}
}

type failingMCPTimeClient struct{}

func (failingMCPTimeClient) ListTools(context.Context, mcpapi.ListToolsRequest) (*mcpapi.ListToolsResult, error) {
	return nil, nil
}
func (failingMCPTimeClient) CallTool(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	return &mcpapi.CallToolResult{IsError: true}, nil
}
func (failingMCPTimeClient) Close() error { return nil }

func TestGatewayFallsBackWhenMCPTimeToolReturnsError(t *testing.T) {
	ctx := ctxWithRoles(context.Background(), "viewer")
	gw := toolkit.NewGateway(ctx)
	gw.SetMCPTimeAdapter(mcpadapter.NewTimeAdapter(failingMCPTimeClient{}))
	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "mcp_time_convert_time",
		Input:    json.RawMessage(`{"source_timezone":"Asia/Shanghai","time":"2026-08-13 09:00:00","target_timezone":"UTC"}`),
	})
	if err != nil {
		t.Fatalf("MCP error should use local fallback: %v", err)
	}
	if !strings.Contains(resp.Output, `"target_timezone":"UTC"`) || !strings.Contains(resp.Output, `"time":"2026-08-13 01:00:00"`) {
		t.Fatalf("unexpected fallback output: %s", resp.Output)
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
	// query_internal_docs must not be exposed until bootstrap has a usable RAG
	// pipeline. Otherwise a model can select a known-unavailable tool and turn
	// an infrastructure degradation into a Chat failure.
	ctx := ctxWithRoles(context.Background(), "operator")
	gw := toolkit.NewGateway(ctx)

	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		ToolName: "query_internal_docs",
		Input:    json.RawMessage(`{"query":"test"}`),
	})
	if err == nil {
		t.Fatal("expected unavailable-tool error")
	}
	if resp != nil {
		t.Fatalf("expected no tool response for an unadvertised adapter, got %#v", resp)
	}
}

func TestOpsAgentSearchLogsToolCallFlow(t *testing.T) {
	var evidence []domain.Evidence
	var steps []string
	ctx := ctxWithRoles(context.Background(), "operator")
	ctx = ctxkeys.WithToolSink(ctx, &evidence)
	ctx = ctxkeys.WithStepSink(ctx, func(stepType, stepName, input, output, status string, latencyMS int64, errMsg string) {
		steps = append(steps, stepType+":"+stepName+":"+status)
	})

	gw := toolkit.NewGateway(ctx)
	einoTools, err := gw.AsEinoTools(ctx, "default", domain.AgentTypeOps)
	if err != nil {
		t.Fatalf("AsEinoTools failed: %v", err)
	}

	var searchLogs tool.InvokableTool
	for _, item := range einoTools {
		info, err := item.Info(ctx)
		if err != nil {
			t.Fatalf("tool Info failed: %v", err)
		}
		if info.Name == "search_logs" {
			invokable, ok := item.(tool.InvokableTool)
			if !ok {
				t.Fatal("search_logs should implement tool.InvokableTool")
			}
			searchLogs = invokable
			break
		}
	}
	if searchLogs == nil {
		t.Fatal("search_logs not found in ops Eino tools")
	}

	output, err := searchLogs.InvokableRun(ctx, `{"query":"mysql deadlock","service":"order-service","level":"ERROR","limit":3}`)
	if err != nil {
		t.Fatalf("search_logs InvokableRun failed: %v", err)
	}
	if !strings.Contains(output, "mysql deadlock detected") {
		t.Fatalf("expected mock log in output, got: %s", output)
	}

	if len(evidence) != 1 {
		t.Fatalf("expected one evidence record, got %d", len(evidence))
	}
	if evidence[0].ToolName != "search_logs" || evidence[0].Status != "success" {
		t.Fatalf("unexpected evidence: %+v", evidence[0])
	}
	if !strings.Contains(evidence[0].Output, `"suppressed":true`) {
		t.Fatalf("expected fail-closed evidence projection, got: %s", evidence[0].Output)
	}
	if len(steps) != 1 || !strings.Contains(steps[0], "tool:search_logs:success") {
		t.Fatalf("expected one successful tool step, got: %+v", steps)
	}
}
