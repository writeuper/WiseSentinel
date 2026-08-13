package metrics

import (
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_http_requests_total",
			Help: "Total HTTP requests processed by the platform.",
		},
		[]string{"method", "path", "code"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

func init() {
	registry.MustRegister(httpRequestsTotal, httpRequestDuration)
}

// HTTPMetrics records request count and latency for Prometheus scraping.
func HTTPMetrics(r *ghttp.Request) {
	start := time.Now()
	r.Middleware.Next()

	status := r.Response.Status
	if status == 0 {
		status = 200
	}

	method := normalizeMethod(r.Method)
	path := normalizePath(r.URL.Path)
	code := strconv.Itoa(status)

	httpRequestsTotal.WithLabelValues(method, path, code).Inc()
	httpRequestDuration.WithLabelValues(method, path).Observe(time.Since(start).Seconds())
}

func normalizeMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return strings.ToUpper(strings.TrimSpace(method))
	default:
		return "OTHER"
	}
}

// normalizePath collapses dynamic path segments to limit metric cardinality.
func normalizePath(path string) string {
	switch path {
	case "/metrics", "/health/live", "/health/ready", "/api.json",
		"/api/v1/sessions", "/api/v1/chat", "/api/v1/chat/stream",
		"/api/v1/knowledge/fault-cards", "/api/v1/admin/vector-gc/tasks",
		"/api/v1/approvals", "/api/v1/admin/agent-configs", "/api/v1/ping",
		"/api/v1/knowledge/documents/upload", "/api/v1/knowledge/documents",
		"/api/v1/ops/analyze", "/api/v1/webhook/alerts", "/api/v1/alertmanager",
		"/api/v1/ops/tasks", "/api/v1/me", "/api/v1/auth/token":
		return path
	}
	if strings.HasPrefix(path, "/api/v1/admin/vector-gc/documents/") {
		segments := strings.Split(strings.TrimPrefix(path, "/api/v1/admin/vector-gc/documents/"), "/")
		if len(segments) == 4 && segments[1] == "tasks" && segments[3] == "redrive" {
			return "/api/v1/admin/vector-gc/documents/{doc_id}/tasks/{target_key}/redrive"
		}
	}

	patterns := []struct {
		prefix     string
		normalized string
	}{
		{"/api/v1/sessions/", "/api/v1/sessions/{id}"},
		{"/api/v1/knowledge/documents/", "/api/v1/knowledge/documents/{id}"},
		{"/api/v1/knowledge/index-tasks/", "/api/v1/knowledge/index-tasks/{id}"},
		{"/api/v1/knowledge/fault-cards/", "/api/v1/knowledge/fault-cards/{id}"},
		{"/api/v1/ops/tasks/", "/api/v1/ops/tasks/{id}"},
		{"/api/v1/approvals/", "/api/v1/approvals/{id}"},
		{"/api/v1/admin/agent-configs/", "/api/v1/admin/agent-configs/{version}"},
		{"/api/v1/traces/", "/api/v1/traces/{trace_id}"},
	}

	for _, p := range patterns {
		if !strings.HasPrefix(path, p.prefix) || len(path) <= len(p.prefix) {
			continue
		}
		suffix := path[len(p.prefix):]
		if idx := strings.Index(suffix, "/"); idx >= 0 {
			return p.normalized + suffix[idx:]
		}
		return p.normalized
	}

	// Request URLs are attacker-controlled. Never use an unknown path as a
	// Prometheus label: scanners or malformed requests could otherwise create
	// one time series per random suffix. Known routes above keep their useful
	// per-endpoint visibility; everything else intentionally coalesces.
	return "/unknown"
}
