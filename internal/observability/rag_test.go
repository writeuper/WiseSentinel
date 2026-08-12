package observability

import "testing"

func TestRAGMetricsUseOnlyStableLowCardinalityLabels(t *testing.T) {
	ObserveRAGRetrieve("success", "high", 0.02)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var foundCounter, foundHistogram bool
	for _, family := range families {
		if family.GetName() != "ws_rag_retrievals_total" && family.GetName() != "ws_rag_retrieval_duration_seconds" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["outcome"] != "success" || labels["confidence"] != "high" || len(labels) != 2 {
				continue
			}
			if family.GetName() == "ws_rag_retrievals_total" && metric.Counter.GetValue() >= 1 {
				foundCounter = true
			}
			if family.GetName() == "ws_rag_retrieval_duration_seconds" && metric.Histogram.GetSampleCount() >= 1 {
				foundHistogram = true
			}
		}
	}
	if !foundCounter || !foundHistogram {
		t.Fatalf("RAG metrics missing: counter=%v histogram=%v", foundCounter, foundHistogram)
	}
}

func TestRAGInventoryMetricsAreUnlabelledAggregates(t *testing.T) {
	SetRAGInventory(2, 7, 1)
	SetRAGPhysicalVectors(42)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"ws_rag_active_documents":        2,
		"ws_rag_active_published_chunks": 7,
		"ws_rag_active_legacy_documents": 1,
		"ws_rag_physical_vectors":        42,
	}
	for _, family := range families {
		expected, ok := want[family.GetName()]
		if !ok {
			continue
		}
		if len(family.Metric) != 1 || len(family.Metric[0].Label) != 0 || family.Metric[0].Gauge.GetValue() != expected {
			t.Fatalf("inventory family %s = %#v", family.GetName(), family.Metric)
		}
		delete(want, family.GetName())
	}
	if len(want) != 0 {
		t.Fatalf("missing inventory metrics: %#v", want)
	}
}

func TestRAGInventoryRefreshErrorsUseBoundedStages(t *testing.T) {
	ObserveRAGInventoryRefreshError("logical")
	ObserveRAGInventoryRefreshError("physical")
	ObserveRAGInventoryRefreshError("secret-stage")
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, family := range families {
		if family.GetName() != "ws_rag_inventory_refresh_errors_total" {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != 1 || metric.Label[0].GetName() != "stage" {
				t.Fatalf("unexpected inventory error labels: %#v", metric.Label)
			}
			stage := metric.Label[0].GetValue()
			if stage != "logical" && stage != "physical" {
				t.Fatalf("unbounded inventory stage %q", stage)
			}
			seen[stage] = metric.Counter.GetValue() >= 1
		}
	}
	if !seen["logical"] || !seen["physical"] {
		t.Fatalf("inventory error stages missing: %#v", seen)
	}
}

func TestMilvusReconnectMetricsUseBoundedOutcomes(t *testing.T) {
	ObserveMilvusReconnect("success")
	ObserveMilvusReconnect("failure")
	ObserveMilvusReconnect("raw-upstream-error")
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, family := range families {
		if family.GetName() != "ws_milvus_reconnects_total" {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != 1 || metric.Label[0].GetName() != "outcome" {
				t.Fatalf("unexpected reconnect labels: %#v", metric.Label)
			}
			outcome := metric.Label[0].GetValue()
			if outcome != "success" && outcome != "failure" {
				t.Fatalf("unbounded reconnect outcome %q", outcome)
			}
			seen[outcome] = metric.Counter.GetValue() >= 1
		}
	}
	if !seen["success"] || !seen["failure"] {
		t.Fatalf("reconnect outcomes missing: %#v", seen)
	}
}
