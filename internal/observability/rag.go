package observability

import "github.com/prometheus/client_golang/prometheus"

var (
	ragRetrieveTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_rag_retrievals_total",
			Help: "Total RAG retrieval attempts by stable outcome and confidence.",
		},
		[]string{"outcome", "confidence"},
	)
	ragRetrieveDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "ws_rag_retrieval_duration_seconds",
			Help: "RAG retrieval latency by stable outcome and confidence.",
			// Keep finer buckets below one second so the development/integration
			// report does not turn a ~200ms retrieval into a misleading 1s P95.
			// The reported percentile remains a histogram bucket upper bound.
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.5, 0.75, 1, 2, 5, 10, 15},
		},
		[]string{"outcome", "confidence"},
	)
	ragActiveDocuments = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_rag_active_documents",
		Help: "Current active RAG documents across all tenants, excluding soft-deleted documents.",
	})
	ragActivePublishedChunks = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_rag_active_published_chunks",
		Help: "Logical chunks in current published generations of active RAG documents; not physical vector rows.",
	})
	ragActiveLegacyDocuments = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_rag_active_legacy_documents",
		Help: "Active RAG documents without a published generation, whose physical vector count is not known from generation state.",
	})
	ragInventoryRefreshErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_rag_inventory_refresh_errors_total",
		Help: "Failures refreshing the aggregate RAG logical inventory before metric exposition.",
	})
	ragPhysicalVectors = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_rag_physical_vectors",
		Help: "Physical rows in the configured Milvus collection across all tenants; may include legacy, superseded and pending-GC vectors.",
	})
)

func init() {
	Registry.MustRegister(ragRetrieveTotal, ragRetrieveDuration, ragActiveDocuments, ragActivePublishedChunks, ragActiveLegacyDocuments, ragInventoryRefreshErrors, ragPhysicalVectors)
}

// ObserveRAGRetrieve records a retrieval without tenant, query, document, or
// provider labels. Those identifiers are high-cardinality and may be sensitive;
// Trace/audit records are the controlled correlation surface instead.
func ObserveRAGRetrieve(outcome, confidence string, seconds float64) {
	if outcome == "" {
		outcome = "error"
	}
	if confidence == "" {
		confidence = "low"
	}
	ragRetrieveTotal.WithLabelValues(outcome, confidence).Inc()
	if seconds >= 0 {
		ragRetrieveDuration.WithLabelValues(outcome, confidence).Observe(seconds)
	}
}

// SetRAGInventory records global logical inventory only. No tenant, document,
// source or embedding-model labels are used.
func SetRAGInventory(activeDocuments, activePublishedChunks, activeLegacyDocuments int) {
	if activeDocuments < 0 || activePublishedChunks < 0 || activeLegacyDocuments < 0 {
		return
	}
	ragActiveDocuments.Set(float64(activeDocuments))
	ragActivePublishedChunks.Set(float64(activePublishedChunks))
	ragActiveLegacyDocuments.Set(float64(activeLegacyDocuments))
}

func ObserveRAGInventoryRefreshError() { ragInventoryRefreshErrors.Inc() }

func SetRAGPhysicalVectors(count int) {
	if count >= 0 {
		ragPhysicalVectors.Set(float64(count))
	}
}
