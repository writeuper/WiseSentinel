// Package observability holds process-wide metrics shared by transport and
// background workers without creating a gateway-to-worker dependency cycle.
package observability

import "github.com/prometheus/client_golang/prometheus"

var Registry = prometheus.NewRegistry()

func init() {
	Registry.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
}
