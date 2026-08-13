package observability

import "github.com/prometheus/client_golang/prometheus"

var auditWriteFailures = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ws_audit_write_failures_total",
		Help: "Audit persistence failures by bounded reason category.",
	}, []string{"reason"})

func init() { Registry.MustRegister(auditWriteFailures) }

// ObserveAuditWriteFailure accepts only a bounded category, never a database
// error, DSN, tenant, trace or request path.
func ObserveAuditWriteFailure(reason string) {
	if reason != "context_canceled" && reason != "database" {
		reason = "database"
	}
	auditWriteFailures.WithLabelValues(reason).Inc()
}
