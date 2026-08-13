package observability

import "github.com/prometheus/client_golang/prometheus"

var alertEventOrphanReaped = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "ws_alert_event_orphan_reaped_total",
	Help: "Alertmanager event reservations removed because their bound Ops task was never persisted.",
})

func init() { Registry.MustRegister(alertEventOrphanReaped) }

func ObserveAlertEventOrphanReaped(count int64) {
	if count > 0 {
		alertEventOrphanReaped.Add(float64(count))
	}
}
