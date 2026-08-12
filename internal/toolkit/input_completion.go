package toolkit

import (
	"context"
	"encoding/json"
	"strings"

	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func completeToolInput(toolName string, raw json.RawMessage, ctx context.Context) json.RawMessage {
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		input = map[string]any{}
	}
	query := ctxkeys.RequestQueryFrom(ctx)
	service := serviceFromText(query)

	if rawInput, ok := input["input"].(string); ok {
		var nested map[string]any
		if json.Unmarshal([]byte(rawInput), &nested) == nil {
			delete(input, "input")
			for key, value := range nested {
				input[key] = value
			}
		} else if strings.TrimSpace(stringValue(input["query"])) == "" {
			input["query"] = rawInput
		}
	}

	switch toolName {
	case "search_logs", "query_logs":
		if strings.TrimSpace(stringValue(input["query"])) == "" && strings.TrimSpace(query) != "" {
			input["query"] = query
		}
		if service != "" && strings.TrimSpace(stringValue(input["service"])) == "" {
			input["service"] = service
		}
	case "query_metric_range":
		if strings.TrimSpace(stringValue(input["query"])) == "" && strings.TrimSpace(query) != "" {
			input["query"] = metricQuery(query, service)
		}
	case "query_logs_by_trace":
		if strings.TrimSpace(stringValue(input["trace_id"])) == "" {
			input["trace_id"] = traceFromText(query)
		}
	case "query_deployments":
		if strings.TrimSpace(stringValue(input["service"])) == "" && service != "" {
			input["service"] = service
		}
	}
	result, _ := json.Marshal(input)
	return result
}

func serviceFromText(text string) string {
	lower := strings.ToLower(text)
	services := []struct {
		name    string
		aliases []string
	}{
		{"order-service", []string{"order-service", "订单服务"}},
		{"payment-service", []string{"payment-service", "支付服务"}},
		{"user-service", []string{"user-service", "用户服务", "用户登录"}},
		{"inventory-service", []string{"inventory-service", "库存服务"}},
		{"gateway", []string{"gateway", "网关"}},
	}
	for _, service := range services {
		for _, alias := range service.aliases {
			if strings.Contains(lower, alias) {
				return service.name
			}
		}
	}
	return ""
}

func traceFromText(text string) string {
	for _, token := range strings.Fields(text) {
		if strings.HasPrefix(strings.ToLower(token), "trace-") {
			return strings.Trim(token, "，。,.!?！？")
		}
	}
	return ""
}

func metricQuery(text, service string) string {
	if service == "" {
		service = "service"
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "延迟") || strings.Contains(lower, "latency"):
		return `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{service="` + service + `"}[5m]))`
	case strings.Contains(lower, "错误率") || strings.Contains(lower, "error") || strings.Contains(lower, "500") || strings.Contains(lower, "503"):
		return `sum(rate(http_requests_total{service="` + service + `",status=~"5.."}[5m])) / sum(rate(http_requests_total{service="` + service + `"}[5m]))`
	default:
		return `up{service="` + service + `"}`
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(toString(value))
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
