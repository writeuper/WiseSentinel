package metrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	opsTaskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_ops_task_duration_seconds",
			Help:    "Ops troubleshooting task latency in seconds.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600, 1800},
		},
		[]string{"stage", "status"},
	)
)

func init() {
	registry.MustRegister(opsTaskDuration)
}

// ObserveOpsTaskDuration records queue, run and end-to-end task durations.
func ObserveOpsTaskDuration(stage, status string, seconds float64) {
	if seconds < 0 {
		return
	}
	opsTaskDuration.WithLabelValues(normalizeOpsStage(stage), normalizeOpsStatus(status)).Observe(seconds)
}

func normalizeOpsStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case "queue", "run", "e2e":
		return strings.TrimSpace(stage)
	default:
		return "other"
	}
}

func normalizeOpsStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pending", "running", "success", "failed", "timeout", "retrying", "abandoned":
		return strings.TrimSpace(status)
	default:
		return "other"
	}
}
