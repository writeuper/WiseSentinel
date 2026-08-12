package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SearchLogsInput is the input for the local mock search_logs tool.
type SearchLogsInput struct {
	Query   string `json:"query"`
	Service string `json:"service,omitempty"`
	Level   string `json:"level,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// SearchLogsOutput is the output for the local mock search_logs tool.
type SearchLogsOutput struct {
	Logs   []MockLogEntry `json:"logs"`
	Count  int            `json:"count"`
	Source string         `json:"source"`
}

// MockLogEntry is a mock log record used for local troubleshooting tests.
type MockLogEntry struct {
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Level     string `json:"level"`
	TraceID   string `json:"trace_id,omitempty"`
	Message   string `json:"message"`
}

var mockLogEntries = []MockLogEntry{
	{
		Timestamp: "2026-07-25T10:01:03+08:00",
		Service:   "order-service",
		Level:     "ERROR",
		TraceID:   "trace-order-500-001",
		Message:   "create order failed: mysql deadlock detected, retry exhausted",
	},
	{
		Timestamp: "2026-07-25T10:01:08+08:00",
		Service:   "order-service",
		Level:     "WARN",
		TraceID:   "trace-order-500-002",
		Message:   "payment callback latency exceeded threshold: 3200ms",
	},
	{
		Timestamp: "2026-07-25T10:02:11+08:00",
		Service:   "payment-service",
		Level:     "ERROR",
		TraceID:   "trace-pay-503-001",
		Message:   "upstream bank gateway returned 503 service unavailable",
	},
	{
		Timestamp: "2026-07-25T10:03:19+08:00",
		Service:   "user-service",
		Level:     "ERROR",
		TraceID:   "trace-user-redis-001",
		Message:   "redis timeout while loading user session",
	},
	{
		Timestamp: "2026-07-25T10:04:27+08:00",
		Service:   "gateway",
		Level:     "WARN",
		TraceID:   "trace-gw-rate-001",
		Message:   "rate limit triggered for tenant default, path=/api/v1/orders",
	},
	{
		Timestamp: "2026-07-25T10:05:41+08:00",
		Service:   "inventory-service",
		Level:     "ERROR",
		TraceID:   "trace-inv-lock-001",
		Message:   "deduct stock failed: distributed lock wait timeout",
	},
	{
		Timestamp: "2026-07-25T10:06:02+08:00",
		Service:   "order-service",
		Level:     "INFO",
		TraceID:   "trace-order-recover-001",
		Message:   "order creation recovered after database connection pool expanded",
	},
}

// NewSearchLogs returns a local mock adapter for searching fault logs.
func NewSearchLogs() func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var req SearchLogsInput
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if strings.TrimSpace(req.Query) == "" {
			return "", fmt.Errorf("query is required")
		}

		limit := req.Limit
		if limit <= 0 {
			limit = 10
		}

		query := normalizeMockQuery(req.Query)
		service := strings.ToLower(req.Service)
		level := strings.ToLower(req.Level)
		logs := make([]MockLogEntry, 0, limit)

		for _, item := range mockLogEntries {
			if service != "" && strings.ToLower(item.Service) != service {
				continue
			}
			if level != "" && strings.ToLower(item.Level) != level {
				continue
			}
			if !matchesMockLog(item, query) {
				continue
			}
			logs = append(logs, item)
			if len(logs) >= limit {
				break
			}
		}

		out := SearchLogsOutput{
			Logs:   logs,
			Count:  len(logs),
			Source: "local_mock",
		}
		if len(out.Logs) == 0 {
			out.Logs = append(out.Logs, MockLogEntry{
				Timestamp: time.Now().Format(time.RFC3339),
				Service:   firstNonEmpty(req.Service, "mock-log"),
				Level:     "INFO",
				Message:   "no matched mock logs found",
			})
			out.Count = len(out.Logs)
		}

		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("marshal output: %w", err)
		}
		return string(result), nil
	}
}

func normalizeMockQuery(query string) string {
	return strings.NewReplacer(
		"网关", "gateway",
		"限流", "rate limit",
		"支付", "payment",
		"库存", "inventory",
		"用户", "user",
		"登录", "login",
		"锁", "lock",
		"错误率", "error rate",
	).Replace(strings.ToLower(query))
}

func matchesMockLog(item MockLogEntry, query string) bool {
	content := strings.ToLower(strings.Join([]string{
		item.Service,
		item.Level,
		item.TraceID,
		item.Message,
	}, " "))
	for _, token := range strings.Fields(query) {
		if strings.Contains(content, token) {
			return true
		}
	}
	return strings.Contains(content, query)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
