package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

const DefaultTimezone = "Asia/Shanghai"
const defaultMaxResponseBytes = 64 * 1024

type TimeAdapter struct {
	client           Client
	maxResponseBytes int
}

func NewTimeAdapter(client Client) *TimeAdapter {
	return NewTimeAdapterWithMaxResponseBytes(client, defaultMaxResponseBytes)
}

func NewTimeAdapterWithMaxResponseBytes(client Client, maxResponseBytes int) *TimeAdapter {
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	return &TimeAdapter{client: client, maxResponseBytes: maxResponseBytes}
}

func (a *TimeAdapter) GetCurrentTime(ctx context.Context, input json.RawMessage) (string, error) {
	args := map[string]any{}
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
	}
	timezone := DefaultTimezone
	if value, ok := args["timezone"]; ok {
		var valid bool
		timezone, valid = value.(string)
		if !valid || strings.TrimSpace(timezone) == "" {
			return "", fmt.Errorf("timezone must be a non-empty string")
		}
	}
	return a.call(ctx, "get_current_time", map[string]any{"timezone": timezone})
}

func (a *TimeAdapter) ConvertTime(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		SourceTimezone string `json:"source_timezone"`
		Time           string `json:"time"`
		TargetTimezone string `json:"target_timezone"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.SourceTimezone) == "" || strings.TrimSpace(args.Time) == "" || strings.TrimSpace(args.TargetTimezone) == "" {
		return "", fmt.Errorf("source_timezone, time, and target_timezone are required")
	}
	return a.call(ctx, "convert_time", map[string]any{
		"source_timezone": args.SourceTimezone,
		"time":            args.Time,
		"target_timezone": args.TargetTimezone,
	})
}

func (a *TimeAdapter) call(ctx context.Context, name string, args map[string]any) (string, error) {
	if a == nil || isNilClient(a.client) {
		return "", fmt.Errorf("MCP time server is unavailable")
	}
	result, err := a.client.CallTool(ctx, mcpapi.CallToolRequest{
		Request: mcpapi.Request{Method: "tools/call"},
		Params:  mcpapi.CallToolParams{Name: name, Arguments: args},
	})
	if err != nil {
		return "", fmt.Errorf("call MCP tool %s: %w", name, err)
	}
	if result == nil {
		return "", fmt.Errorf("MCP tool %s returned no result", name)
	}
	if result.IsError {
		return "", fmt.Errorf("MCP tool %s returned an error", name)
	}
	parts := make([]string, 0, len(result.Content))
	responseBytes := 0
	for _, content := range result.Content {
		if text, ok := content.(mcpapi.TextContent); ok {
			responseBytes += len(text.Text)
			if len(parts) > 0 {
				responseBytes++ // newline added by Join
			}
			if responseBytes > a.maxResponseBytes {
				return "", fmt.Errorf("MCP tool %s response exceeds configured size limit", name)
			}
			parts = append(parts, text.Text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("MCP tool %s returned no text content", name)
	}
	return strings.Join(parts, "\n"), nil
}

func isNilClient(c Client) bool {
	if c == nil {
		return true
	}
	value := reflect.ValueOf(c)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
