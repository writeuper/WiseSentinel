package observability

import (
	"testing"
)

func TestVectorGCMetricsExposeLowCardinalityStateAndOutcome(t *testing.T) {
	SetVectorGCTasks("dead", 3)
	ObserveVectorGCAttempt("generation", "dead")
	SetVectorGCDeadRecent(4)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var foundGauge, foundCounter, foundRecent bool
	for _, family := range families {
		for _, metric := range family.Metric {
			if family.GetName() == "ws_rag_vector_gc_dead_tasks_24h" && metric.Gauge.GetValue() == 4 {
				foundRecent = true
			}
			for _, label := range metric.Label {
				if label.GetName() != "status" || label.GetValue() != "dead" {
					continue
				}
				if family.GetName() == "ws_rag_vector_gc_tasks" && metric.Gauge.GetValue() == 3 {
					foundGauge = true
				}
			}
			if family.GetName() == "ws_rag_vector_gc_attempts_total" {
				var kind, outcome string
				for _, label := range metric.Label {
					if label.GetName() == "target_kind" {
						kind = label.GetValue()
					}
					if label.GetName() == "outcome" {
						outcome = label.GetValue()
					}
				}
				if kind == "generation" && outcome == "dead" && metric.Counter.GetValue() >= 1 {
					foundCounter = true
				}
			}
		}
	}
	if !foundGauge || !foundCounter || !foundRecent {
		t.Fatalf("GC metrics missing: gauge=%v counter=%v recent=%v", foundGauge, foundCounter, foundRecent)
	}
}
