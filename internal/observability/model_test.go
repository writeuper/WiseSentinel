package observability

import "testing"

func TestModelMetricsUseOnlyBoundedLabels(t *testing.T) {
	ObserveModelCall("untrusted-provider-https://example.invalid/tenant-secret", "unexpected-operation", "raw upstream error", 0.25)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_model_calls_total" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["provider"] == "other" && labels["operation"] == "other" && labels["outcome"] == "upstream_error" {
				return
			}
		}
	}
	t.Fatal("bounded model metric labels not found")
}

func TestModelLatencyHistogramHasLongTailPrecisionBuckets(t *testing.T) {
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_model_call_duration_seconds" {
			continue
		}
		if len(family.Metric) == 0 || family.Metric[0].Histogram == nil {
			t.Fatal("model latency histogram missing")
		}
		seen := map[float64]bool{}
		for _, bucket := range family.Metric[0].Histogram.Bucket {
			seen[bucket.GetUpperBound()] = true
		}
		for _, want := range []float64{3, 4, 6, 8, 12, 15, 18} {
			if !seen[want] {
				t.Errorf("model histogram missing long-tail bucket %v", want)
			}
		}
		return
	}
	t.Fatal("model latency histogram family not found")
}

func TestModelAdmissionMetricsUseBoundedProviderAndBalanceInFlight(t *testing.T) {
	release := ObserveModelAdmission("untrusted-provider-https://example.invalid/tenant-secret", true)
	ObserveModelAdmission("untrusted-provider-https://example.invalid/tenant-secret", false)
	release()
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var sawGauge, sawRejection bool
	for _, family := range families {
		for _, metric := range family.Metric {
			provider := ""
			for _, label := range metric.Label {
				if label.GetName() == "provider" {
					provider = label.GetValue()
				}
			}
			if provider != "other" {
				continue
			}
			switch family.GetName() {
			case "ws_model_admission_in_flight":
				sawGauge = metric.Gauge.GetValue() == 0
			case "ws_model_admission_rejections_total":
				sawRejection = metric.Counter.GetValue() >= 1
			}
		}
	}
	if !sawGauge || !sawRejection {
		t.Fatalf("admission metrics missing or unbalanced: gauge=%t rejected=%t", sawGauge, sawRejection)
	}
}

func TestModelAdmissionOutcomeIsBounded(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"accepted", "accepted"},
		{"rejected", "rejected"},
		{"canceled", "canceled"},
		{"tenant-a-secret", "rejected"},
	} {
		if got := modelAdmissionOutcome(tc.input); got != tc.want {
			t.Fatalf("modelAdmissionOutcome(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestModelBreakerEventsUseBoundedLabels(t *testing.T) {
	ObserveModelBreakerEvent("provider-with-secret", "open")
	ObserveModelBreakerEvent("provider-with-secret", "unknown")
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_model_breaker_events_total" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["provider"] == "other" && (labels["event"] == "open" || labels["event"] == "rejected") {
				return
			}
		}
	}
	t.Fatal("bounded breaker metric labels not found")
}

func TestModelTokenMetricsUseBoundedLabelsAndIgnoreMissingUsage(t *testing.T) {
	ObserveModelTokens("provider-with-secret", "generate", 12, 7, 19)
	ObserveModelTokens("provider-with-secret", "stream_complete", 0, -1, 0)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_model_tokens_total" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["provider"] == "other" && labels["operation"] == "generate" && metric.Counter.GetValue() > 0 {
				return
			}
		}
	}
	t.Fatal("bounded model token metric not found")
}

func TestModelStreamTTFTUsesBoundedProviderAndIgnoresNegativeValues(t *testing.T) {
	ObserveModelStreamTTFT("provider-with-secret", -1)
	ObserveModelStreamTTFT("provider-with-secret", 0.125)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_model_stream_ttft_seconds" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["provider"] == "other" && metric.Histogram != nil && metric.Histogram.GetSampleCount() >= 1 {
				return
			}
		}
	}
	t.Fatal("bounded stream TTFT metric not found")
}
