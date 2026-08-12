package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/configx"
)

// LogsByTraceInput is the input for query_logs_by_trace.
type LogsByTraceInput struct {
	TraceID string `json:"trace_id"`
	Service string `json:"service,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// LogsByTraceOutput is the formatted output for query_logs_by_trace.
type LogsByTraceOutput struct {
	Logs    []TraceLogEntry `json:"logs"`
	Count   int             `json:"count"`
	Source  string          `json:"source"`
	TraceID string          `json:"trace_id"`
}

// TraceLogEntry is a single log line bound to a trace.
type TraceLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level,omitempty"`
	Service   string `json:"service,omitempty"`
	Message   string `json:"message"`
}

// NewQueryLogsByTrace returns an adapter that queries logs by TraceID via MCP.
func NewQueryLogsByTrace() func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var req LogsByTraceInput
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if req.TraceID == "" {
			return "", fmt.Errorf("trace_id is required")
		}

		mcpURL := configx.String(ctx, "mcp.log.url", "MCP_LOG_URL")
		if strings.EqualFold(mcpURL, "local_mock") {
			return searchLogsByTrace(ctx, req)
		}
		if mcpURL == "" {
			out := LogsByTraceOutput{
				Source:  "mcp_not_configured",
				TraceID: req.TraceID,
			}
			raw, _ := json.Marshal(out)
			return string(raw), nil
		}

		payload := map[string]interface{}{
			"query":    req.TraceID,
			"trace_id": req.TraceID,
		}
		if req.Service != "" {
			payload["service"] = req.Service
		}
		if req.Limit > 0 {
			payload["limit"] = req.Limit
		}
		body, _ := json.Marshal(payload)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, strings.NewReader(string(body)))
		if err != nil {
			return "", fmt.Errorf("create MCP request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			return "", fmt.Errorf("MCP request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", upstreamHTTPError("MCP", resp)
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("read MCP response: %w", err)
		}
		// Try structured shape first (reuse QueryLogsOutput), fallback to raw text.
		var logs QueryLogsOutput
		if err := json.Unmarshal(respBody, &logs); err != nil {
			logs = QueryLogsOutput{
				Logs: []LogEntry{
					{Timestamp: time.Now().UTC().Format(time.RFC3339), Message: string(respBody)},
				},
				Count:  1,
				Source: "mcp",
			}
		}
		logs.Source = "mcp_trace"
		if logs.Count == 0 && len(logs.Logs) > 0 {
			logs.Count = len(logs.Logs)
		}

		// Project into trace-bound shape so downstream can rely on it.
		out := LogsByTraceOutput{
			Source:  logs.Source,
			TraceID: req.TraceID,
		}
		for _, l := range logs.Logs {
			out.Logs = append(out.Logs, TraceLogEntry{
				Timestamp: l.Timestamp,
				Level:     l.Level,
				Message:   l.Message,
			})
		}
		out.Count = len(out.Logs)

		raw, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("marshal output: %w", err)
		}
		return string(raw), nil
	}
}

func searchLogsByTrace(ctx context.Context, req LogsByTraceInput) (string, error) {
	output, err := NewSearchLogs()(ctx, mustJSON(map[string]any{
		"query":   req.TraceID,
		"service": req.Service,
		"limit":   req.Limit,
	}))
	if err != nil {
		return "", err
	}
	var logs SearchLogsOutput
	if err := json.Unmarshal([]byte(output), &logs); err != nil {
		return "", err
	}
	out := LogsByTraceOutput{Source: logs.Source, TraceID: req.TraceID, Count: logs.Count}
	for _, item := range logs.Logs {
		out.Logs = append(out.Logs, TraceLogEntry{
			Timestamp: item.Timestamp,
			Level:     item.Level,
			Service:   item.Service,
			Message:   item.Message,
		})
	}
	if out.Count == 0 {
		out.Count = len(out.Logs)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
