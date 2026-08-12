package ops

import "testing"

func TestFocusedToolRequestsUsesAlertsOnlyForCurrentFiringAlerts(t *testing.T) {
	requests, handled := focusedToolRequests("当前有哪些 firing 告警需要处理")
	if !handled || len(requests) != 1 || requests[0].name != "query_prometheus_alerts" {
		t.Fatalf("focused requests = %#v, handled=%v; want only query_prometheus_alerts", requests, handled)
	}
}
