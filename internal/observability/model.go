package observability

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	modelCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_model_calls_total",
			Help: "Completed model invocation phases by provider class, operation and outcome.",
		},
		[]string{"provider", "operation", "outcome"},
	)
	modelCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_model_call_duration_seconds",
			Help:    "Model invocation phase latency by provider class, operation and outcome.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300},
		},
		[]string{"provider", "operation", "outcome"},
	)
	modelAdmissionInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_model_admission_in_flight",
			Help: "In-flight model calls holding a local provider admission slot.",
		},
		[]string{"provider"},
	)
	modelAdmissionRejected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_model_admission_rejections_total",
			Help: "Model calls rejected because the local provider admission budget was full.",
		},
		[]string{"provider"},
	)
	modelAdmissionWait = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_model_admission_wait_seconds",
			Help:    "Local admission decision time before a model invocation, by bounded provider class and decision.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2},
		},
		[]string{"provider", "outcome"},
	)
)

func init() {
	Registry.MustRegister(modelCalls, modelCallDuration, modelAdmissionInFlight, modelAdmissionRejected, modelAdmissionWait)
}

// ObserveModelAdmissionWait records only the local time spent deciding
// admission. Current admission is fail-fast, so accepted/rejected samples are
// expected to be near zero; recording that fact keeps execution latency from
// being conflated with a future bounded-queue wait.
func ObserveModelAdmissionWait(provider, outcome string, seconds float64) {
	if seconds < 0 {
		return
	}
	modelAdmissionWait.WithLabelValues(modelProviderClass(provider), modelAdmissionOutcome(outcome)).Observe(seconds)
}

// ObserveModelCall records only a bounded provider class and result category.
// Prompt text, model deployment names, tenant IDs and upstream error strings
// are deliberately excluded to avoid sensitive or high-cardinality labels.
func ObserveModelCall(provider, operation, outcome string, seconds float64) {
	provider = modelProviderClass(provider)
	operation = modelOperation(operation)
	outcome = modelOutcome(outcome)
	modelCalls.WithLabelValues(provider, operation, outcome).Inc()
	if seconds >= 0 {
		modelCallDuration.WithLabelValues(provider, operation, outcome).Observe(seconds)
	}
}

// ObserveModelAdmission records local admission occupancy and capacity
// rejections using only the bounded provider class. Model, tenant, key and
// prompt are deliberately excluded from labels.
func ObserveModelAdmission(provider string, accepted bool) func() {
	provider = modelProviderClass(provider)
	if !accepted {
		modelAdmissionRejected.WithLabelValues(provider).Inc()
		return func() {}
	}
	modelAdmissionInFlight.WithLabelValues(provider).Inc()
	return func() { modelAdmissionInFlight.WithLabelValues(provider).Dec() }
}

func modelProviderClass(provider string) string {
	p := strings.ToLower(provider)
	switch {
	case strings.Contains(p, "dashscope"):
		return "dashscope"
	case strings.Contains(p, "volces") || strings.Contains(p, "ark"):
		return "ark"
	case strings.Contains(p, "azure"):
		return "azure_openai"
	case strings.Contains(p, "openai"):
		return "openai"
	default:
		return "other"
	}
}

func modelOperation(operation string) string {
	switch operation {
	case "generate", "stream_handshake", "stream_complete":
		return operation
	default:
		return "other"
	}
}

func modelOutcome(outcome string) string {
	switch outcome {
	case "success", "timeout", "canceled", "overloaded", "upstream_error":
		return outcome
	default:
		return "upstream_error"
	}
}

func modelAdmissionOutcome(outcome string) string {
	switch outcome {
	case "accepted", "rejected", "canceled":
		return outcome
	default:
		return "rejected"
	}
}
