package ops

import "testing"

func TestFocusedToolRequestsUsesAlertsOnlyForCurrentFiringAlerts(t *testing.T) {
	requests, handled := focusedToolRequests("当前有哪些 firing 告警需要处理")
	if !handled || len(requests) != 1 || requests[0].name != "query_prometheus_alerts" {
		t.Fatalf("focused requests = %#v, handled=%v; want only query_prometheus_alerts", requests, handled)
	}
}

func TestFocusedToolRequestsHonorExplicitForbiddenDeployment(t *testing.T) {
	requests, handled := focusedToolRequests("order-service 500 错误，请只读查询日志和指标，不要查询发布记录。")
	if !handled || len(requests) != 2 || requests[0].name != "search_logs" || requests[1].name != "query_metric_range" {
		t.Fatalf("requests = %#v, handled=%v; want logs+metrics only", requests, handled)
	}
}

func TestFocusedToolRequestsHonorPrometheusDegradation(t *testing.T) {
	requests, handled := focusedToolRequests("payment-service 503，请在 Prometheus 不可用时基于日志给出降级结论。")
	if !handled || len(requests) != 1 || requests[0].name != "search_logs" {
		t.Fatalf("requests = %#v, handled=%v; want logs only", requests, handled)
	}
}

func TestFocusedToolRequestsHonorRollbackPermissionBoundary(t *testing.T) {
	requests, handled := focusedToolRequests("gateway 限流，请说明权限不足时不能执行回滚，并给出升级路径。")
	if !handled || len(requests) != 2 || requests[0].name != "search_logs" || requests[1].name != "query_metric_range" {
		t.Fatalf("requests = %#v, handled=%v; want logs+metrics only", requests, handled)
	}
}

func TestFocusedToolRequestsPrioritizeExplicitLogLookup(t *testing.T) {
	requests, handled := focusedToolRequests("inventory-service 库存同步延迟，请检索日志确认是否存在 replication lag 并给出根因。")
	if !handled || len(requests) != 1 || requests[0].name != "search_logs" {
		t.Fatalf("requests = %#v, handled=%v; want logs only", requests, handled)
	}
}
