package metrics

import (
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var registry = prometheus.NewRegistry()

func init() {
	registry.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
}

// Register mounts the Prometheus /metrics endpoint.
func Register(s *ghttp.Server) {
	s.BindHandler("/metrics", func(r *ghttp.Request) {
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(r.Response.Writer, r.Request)
	})
}
