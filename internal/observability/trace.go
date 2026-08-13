package observability

import "github.com/prometheus/client_golang/prometheus"

var staleTraceReaped = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "ws_agent_trace_stale_reaped_total",
	Help: "Agent traces marked abandoned during stale-running recovery.",
})

func init() { Registry.MustRegister(staleTraceReaped) }

func ObserveStaleTraceReaped(count int64) {
	if count > 0 {
		staleTraceReaped.Add(float64(count))
	}
}
