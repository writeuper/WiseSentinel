package observability

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	toolCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_tool_calls_total",
		Help: "Tool gateway calls by bounded tool, agent and outcome.",
	}, []string{"tool", "agent", "outcome"})
	toolCallDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ws_tool_call_duration_seconds",
		Help:    "Tool gateway execution latency by bounded tool, agent and outcome.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	}, []string{"tool", "agent", "outcome"})
)

func init() { Registry.MustRegister(toolCalls, toolCallDuration) }

// ObserveToolCall normalizes labels before export so an attacker-controlled
// tool name, agent type or error string cannot create unbounded Prometheus
// cardinality. Tool names in the configured allowlist are retained.
func ObserveToolCall(tool, agent, outcome string, seconds float64) {
	tool = boundedTool(tool)
	agent = boundedAgent(agent)
	outcome = boundedToolOutcome(outcome)
	toolCalls.WithLabelValues(tool, agent, outcome).Inc()
	if seconds >= 0 {
		toolCallDuration.WithLabelValues(tool, agent, outcome).Observe(seconds)
	}
}

func boundedTool(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 128 || strings.ContainsAny(v, "\n\r") {
		return "other"
	}
	return v
}

func boundedAgent(v string) string {
	switch strings.TrimSpace(v) {
	case "chat", "ops":
		return strings.TrimSpace(v)
	default:
		return "other"
	}
}

func boundedToolOutcome(v string) string {
	switch strings.TrimSpace(v) {
	case "success", "error", "rejected", "unavailable", "timeout":
		return strings.TrimSpace(v)
	default:
		return "error"
	}
}
