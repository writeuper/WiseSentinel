package mcp

import (
	"context"
	"strings"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

type fakeClient struct{ result *mcpapi.CallToolResult }

func (f fakeClient) ListTools(context.Context, mcpapi.ListToolsRequest) (*mcpapi.ListToolsResult, error) {
	return nil, nil
}
func (f fakeClient) CallTool(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	return f.result, nil
}
func (f fakeClient) Close() error { return nil }

func TestTimeAdapterRejectsOversizedMCPText(t *testing.T) {
	adapter := NewTimeAdapterWithMaxResponseBytes(fakeClient{result: &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.TextContent{Text: "12345"}},
	}}, 4)
	_, err := adapter.GetCurrentTime(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds configured size limit") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestTimeAdapterJoinsTextWithinConfiguredLimit(t *testing.T) {
	adapter := NewTimeAdapterWithMaxResponseBytes(fakeClient{result: &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.TextContent{Text: "12"}, mcpapi.TextContent{Text: "34"}},
	}}, 5)
	got, err := adapter.GetCurrentTime(context.Background(), nil)
	if err != nil || got != "12\n34" {
		t.Fatalf("bounded response = %q, %v", got, err)
	}
}
