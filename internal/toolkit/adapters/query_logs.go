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

// QueryLogsInput is the input for query_logs.
type QueryLogsInput struct {
	Query   string `json:"query"`
	Region  string `json:"region,omitempty"`
	TopicID string `json:"topic_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// QueryLogsOutput is the output for query_logs.
type QueryLogsOutput struct {
	Logs   []LogEntry `json:"logs"`
	Count  int        `json:"count"`
	Source string     `json:"source"`
}

// LogEntry is a single log line.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Level     string `json:"level,omitempty"`
}

// NewQueryLogs returns an adapter that queries logs via MCP endpoint.
func NewQueryLogs() func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var req QueryLogsInput
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}

		mcpURL := configx.String(ctx, "mcp.log.url", "MCP_LOG_URL")
		if mcpURL == "" {
			// Return a meaningful message when MCP is not configured
			output := QueryLogsOutput{
				Logs:   nil,
				Count:  0,
				Source: "mcp_not_configured",
			}
			raw, _ := json.Marshal(output)
			return string(raw), nil
		}

		// Build MCP query request
		payload := map[string]interface{}{
			"query": req.Query,
		}
		if req.Region != "" {
			payload["region"] = req.Region
		}
		if req.TopicID != "" {
			payload["topic_id"] = req.TopicID
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

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("read MCP response: %w", err)
		}
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		// Try to parse as structured output; fallback to raw text
		var logs QueryLogsOutput
		if err := json.Unmarshal(respBody, &logs); err != nil {
			// Return raw response
			logs = QueryLogsOutput{
				Logs: []LogEntry{
					{Timestamp: time.Now().UTC().Format(time.RFC3339), Message: string(respBody)},
				},
				Count:  1,
				Source: "mcp",
			}
		}
		logs.Source = "mcp"
		if logs.Count == 0 && len(logs.Logs) > 0 {
			logs.Count = len(logs.Logs)
		}

		result, err := json.Marshal(logs)
		if err != nil {
			return "", fmt.Errorf("marshal output: %w", err)
		}
		return string(result), nil
	}
}