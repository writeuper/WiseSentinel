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
	opsTaskTimeouts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_ops_task_timeouts_total",
		Help: "Ops tasks fenced into timeout by the worker reaper.",
	})
	opsTaskReaperRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_ops_task_reaper_runs_total",
			Help: "Ops task timeout reaper cycles by bounded outcome.",
		},
		[]string{"outcome"},
	)
)

func init() {
	registry.MustRegister(opsTaskDuration, opsTaskTimeouts, opsTaskReaperRuns)
}

// ObserveOpsTaskDuration records queue, run and end-to-end task durations.
func ObserveOpsTaskDuration(stage, status string, seconds float64) {
	if seconds < 0 {
		return
	}
	opsTaskDuration.WithLabelValues(normalizeOpsStage(stage), normalizeOpsStatus(status)).Observe(seconds)
}

// ObserveOpsTaskTimeouts records the number of running tasks fenced into a
// timeout during one worker reaper cycle. Negative values are ignored so a
// repository/accounting bug cannot decrement the monotonic counter.
func ObserveOpsTaskTimeouts(count int64) {
	if count > 0 {
		opsTaskTimeouts.Add(float64(count))
	}
}

// ObserveOpsTaskReaperRun records whether the timeout sweep completed or
// failed. The label is deliberately bounded and contains no database error.
func ObserveOpsTaskReaperRun(outcome string) {
	opsTaskReaperRuns.WithLabelValues(normalizeReaperOutcome(outcome)).Inc()
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
	case "pending", "running", "success", "failed", "timeout", "retrying", "abandoned", "incomplete", "cancelled":
		return strings.TrimSpace(status)
	default:
		return "other"
	}
}

func normalizeReaperOutcome(outcome string) string {
	switch strings.TrimSpace(outcome) {
	case "success", "error":
		return strings.TrimSpace(outcome)
	default:
		return "other"
	}
}
