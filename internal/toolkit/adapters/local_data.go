package adapters

import (
	"encoding/json"
	"strings"
	"time"
)

func localMockAlerts() string {
	out := PrometheusAlertsOutput{
		Alerts: []PrometheusAlert{
			{
				Name:        "OrderServiceHighErrorRate",
				State:       "firing",
				Severity:    "critical",
				Description: "order-service 5xx error rate is above threshold",
				Labels:      map[string]string{"service": "order-service", "severity": "critical"},
				StartsAt:    "2026-07-25T10:00:00+08:00",
			},
			{
				Name:        "PaymentServiceUnavailable",
				State:       "firing",
				Severity:    "warning",
				Description: "payment-service upstream dependency is unavailable",
				Labels:      map[string]string{"service": "payment-service", "severity": "warning"},
				StartsAt:    "2026-07-25T10:02:00+08:00",
			},
		},
		Source: "local_mock",
		IsMock: true,
	}
	return marshalLocal(out)
}

func localMockMetrics(req MetricRangeInput) string {
	service := serviceFromQuery(req.Query)
	if service == "" {
		service = "order-service"
	}
	metric := "http_requests_error_rate"
	if strings.Contains(strings.ToLower(req.Query), "latency") || strings.Contains(strings.ToLower(req.Query), "duration") {
		metric = "http_request_duration_p95_seconds"
	}
	now := time.Now().Unix()
	points := make([]MetricPoint, 0, 6)
	for i := 5; i >= 0; i-- {
		value := 0.02 + float64(5-i)*0.01
		if metric != "http_requests_error_rate" {
			value = 0.18 + float64(5-i)*0.03
		}
		points = append(points, MetricPoint{Timestamp: float64(now - int64(i*300)), Value: value})
	}
	return marshalLocal(MetricRangeOutput{
		Series: []MetricSeries{{Metric: map[string]string{"service": service, "metric": metric}, Points: points}},
		Count:  1,
		Status: "success",
		Source: "local_mock",
		IsMock: true,
	})
}

func localMockDeployments(req DeploymentsInput) string {
	service := req.Service
	if service == "" {
		service = "order-service"
	}
	return marshalLocal(DeploymentsOutput{
		Deployments: []DeploymentRecord{{
			ReleaseID: "release-local-001",
			Service:   service,
			Env:       "prod",
			Version:   "v2026.07.25.1",
			Operator:  "local-fixture",
			Status:    "success",
			Strategy:  "rolling",
			StartTime: "2026-07-25T09:30:00+08:00",
			EndTime:   "2026-07-25T09:35:00+08:00",
			CommitID:  "local-commit-001",
			CommitMsg: "adjust database connection pool",
		}},
		Count:  1,
		Source: "local_mock",
	})
}

func serviceFromQuery(query string) string {
	for _, service := range []string{"order-service", "payment-service", "user-service", "inventory-service", "gateway"} {
		if strings.Contains(strings.ToLower(query), service) {
			return service
		}
	}
	return ""
}

func marshalLocal(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
