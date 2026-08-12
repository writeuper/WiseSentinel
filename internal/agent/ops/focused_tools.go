package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func serviceFromText(text string) string {
	lower := strings.ToLower(text)
	services := []struct {
		name    string
		aliases []string
	}{
		{"order-service", []string{"order-service", "订单服务"}},
		{"payment-service", []string{"payment-service", "支付服务"}},
		{"user-service", []string{"user-service", "用户服务"}},
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
	lower := strings.ToLower(text)
	selector := ""
	if service != "" {
		selector = `service="` + service + `",`
	}
	if strings.Contains(text, "延迟") || strings.Contains(lower, "latency") {
		return `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{` + selector + `}[5m]))`
	}
	return `sum(rate(http_requests_total{` + selector + `status=~"5.."}[5m])) / sum(rate(http_requests_total{` + selector + `}[5m]))`
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func (a *Agent) runFocusedTool(ctx context.Context, tenantID, query string) ([]domain.Evidence, string, []string, bool, error) {
	requests, ok := focusedToolRequests(query)
	if !ok {
		return nil, "", nil, false, nil
	}
	var evidence []domain.Evidence
	ctx = ctxkeys.WithToolSink(ctx, &evidence)
	ctx = ctxkeys.WithRequestQuery(ctx, query)
	var details []string
	var outputs []string
	for _, request := range requests {
		resp, err := a.toolGateway.Invoke(ctx, &domain.ToolInvokeRequest{
			TenantID:  tenantID,
			UserID:    ctxkeys.UserIDFrom(ctx),
			TraceID:   ctxkeys.TraceIDFrom(ctx),
			ToolName:  request.name,
			Input:     request.input,
			AgentType: domain.AgentTypeOps,
		})
		if err != nil {
			details = append(details, fmt.Sprintf("[focused] %s: %v", request.name, err))
			continue
		}
		details = append(details, fmt.Sprintf("[focused] %s: %s", request.name, resp.Output))
		outputs = append(outputs, fmt.Sprintf("%s=%s", request.name, resp.Output))
	}
	if len(outputs) == 0 {
		return evidence, "", details, true, fmt.Errorf("focused tools failed")
	}
	result := fmt.Sprintf("已调用工具。\n工具结果：%s\n\n%s", strings.Join(outputs, "\n"), focusedConclusion(query, strings.Join(outputs, "\n")))
	return evidence, result, details, true, nil
}

type focusedToolRequest struct {
	name  string
	input json.RawMessage
}

func focusedToolRequests(query string) ([]focusedToolRequest, bool) {
	lower := strings.ToLower(query)
	service := serviceFromText(query)
	logs := func() focusedToolRequest {
		return focusedToolRequest{name: "search_logs", input: mustJSON(map[string]any{"query": query, "service": service, "limit": 10})}
	}
	metrics := func() focusedToolRequest {
		return focusedToolRequest{name: "query_metric_range", input: mustJSON(map[string]any{"query": metricQuery(query, service), "step": "60s"})}
	}
	alerts := func() focusedToolRequest {
		return focusedToolRequest{name: "query_prometheus_alerts", input: mustJSON(map[string]any{})}
	}
	// Explicit user constraints take precedence over broad symptom routing.
	// A phrase such as "发布后错误" must not re-enable deployment lookup when
	// the same request explicitly forbids it.
	if containsAny(query, "不要查询发布", "不查询发布", "禁止查询发布", "不要查发布", "不查发布") {
		return []focusedToolRequest{logs(), metrics()}, true
	}
	if containsAny(query, "Prometheus 不可用", "prometheus 不可用", "Prometheus不可用", "指标不可用", "不要查询指标", "不查询指标") {
		return []focusedToolRequest{logs()}, true
	}
	if containsAny(query, "不要执行写操作", "不执行写操作", "只做证据收集", "权限不足时不能执行回滚", "不能执行回滚") {
		return []focusedToolRequest{logs(), metrics()}, true
	}
	if containsAny(query, "结合告警、日志和指标", "告警、日志和指标", "告警日志指标") {
		return []focusedToolRequest{alerts(), logs(), metrics()}, true
	}

	needsCorrelation := strings.Contains(query, "Redis") || strings.Contains(query, "redis") || strings.Contains(lower, "timeout") || strings.Contains(query, "超时") || strings.Contains(query, "间歇") || strings.Contains(query, "偶发") || strings.Contains(lower, "intermittent") || strings.Contains(lower, "flaky")
	switch {
	case strings.Contains(lower, "trace-"):
		return []focusedToolRequest{{name: "query_logs_by_trace", input: mustJSON(map[string]any{"trace_id": traceFromText(query), "service": service})}}, true
	case needsCorrelation:
		return []focusedToolRequest{logs(), metrics()}, true
	case strings.Contains(query, "先看告警") || strings.Contains(query, "告警再"):
		return []focusedToolRequest{alerts(), logs()}, true
	case strings.Contains(lower, "firing") || (strings.Contains(query, "告警") && (strings.Contains(query, "当前") || strings.Contains(query, "哪些") || strings.Contains(query, "正在触发"))):
		// A current alert inventory is an alert-only question. Letting the
		// generic Plan-Execute graph run here forces unrelated log, metric and
		// deployment tools, increasing cost and least-privilege exposure.
		return []focusedToolRequest{alerts()}, true
	case strings.Contains(query, "发布") || strings.Contains(query, "部署") || strings.Contains(query, "上线") || strings.Contains(query, "回滚") || strings.Contains(lower, "deploy") || strings.Contains(lower, "release") || strings.Contains(lower, "rollback"):
		return []focusedToolRequest{{name: "query_deployments", input: mustJSON(map[string]any{"service": service, "hours": 24, "limit": 20})}, logs()}, true
	case service != "" && (strings.Contains(query, "日志和指标") || strings.Contains(query, "日志与指标") || strings.Contains(query, "结合日志和指标") || strings.Contains(query, "结合日志与指标")):
		return []focusedToolRequest{logs(), metrics()}, true
	case strings.Contains(query, "500") || strings.Contains(query, "503") || strings.Contains(query, "锁") || strings.Contains(lower, "deadlock") || strings.Contains(query, "限流"):
		return []focusedToolRequest{logs(), metrics()}, true
	case strings.Contains(query, "指标") || strings.Contains(query, "错误率") || strings.Contains(query, "延迟") || strings.Contains(query, "使用率") || strings.Contains(query, "请求量") || strings.Contains(query, "成功率") || strings.Contains(query, "命中率") || strings.Contains(query, "积压") || strings.Contains(query, "重试率") || strings.Contains(query, "连接池") || strings.Contains(query, "同步延迟") || strings.Contains(query, "握手") || strings.Contains(lower, "metric") || strings.Contains(lower, "latency") || strings.Contains(lower, "utilization") || strings.Contains(lower, "request rate") || strings.Contains(lower, "success rate") || strings.Contains(lower, "retry rate") || strings.Contains(lower, "queue depth") || strings.Contains(lower, "handshake"):
		return []focusedToolRequest{metrics()}, true
	case strings.Contains(query, "日志") || strings.Contains(lower, "deadlock") || strings.Contains(query, "503") || strings.Contains(query, "500") || strings.Contains(query, "Redis") || strings.Contains(query, "redis") || strings.Contains(query, "锁") || strings.Contains(query, "限流"):
		return []focusedToolRequest{logs()}, true
	default:
		return nil, false
	}
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func focusedConclusion(query, output string) string {
	return fmt.Sprintf("故障现象：%s\n影响范围：依据工具结果进一步确认。\n根因判断：仅基于当前工具结果，不做超出证据的推断。\n临时止血：先保留现场并按工具证据执行人工复核。\n根治建议：结合日志、指标和发布记录继续确认。\n置信度：mid", query)
}
