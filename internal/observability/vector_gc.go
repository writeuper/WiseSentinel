package observability

import "github.com/prometheus/client_golang/prometheus"

var (
	vectorGCTasks = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_rag_vector_gc_tasks",
			Help: "Current durable RAG vector-GC tasks by state.",
		},
		[]string{"status"},
	)
	vectorGCAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_rag_vector_gc_attempts_total",
			Help: "Completed RAG vector-GC attempts by target type and outcome.",
		},
		[]string{"target_kind", "outcome"},
	)
	vectorGCDeadRecent = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_rag_vector_gc_dead_tasks_24h",
		Help: "Vector-GC tasks transitioned to dead within the last 24 hours.",
	})
)

func init() { Registry.MustRegister(vectorGCTasks, vectorGCAttempts, vectorGCDeadRecent) }

// SetVectorGCTasks reports aggregate, deliberately tenant-free queue state.
func SetVectorGCTasks(status string, count int) {
	if count < 0 {
		return
	}
	vectorGCTasks.WithLabelValues(status).Set(float64(count))
}

func ObserveVectorGCAttempt(targetKind, outcome string) {
	vectorGCAttempts.WithLabelValues(targetKind, outcome).Inc()
}

func SetVectorGCDeadRecent(count int) {
	if count >= 0 {
		vectorGCDeadRecent.Set(float64(count))
	}
}
