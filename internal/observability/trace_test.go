package observability

import "testing"

func TestStaleTraceReapedMetricIsBoundedAndMonotonic(t *testing.T) {
	ObserveStaleTraceReaped(3)
	ObserveStaleTraceReaped(0)
	ObserveStaleTraceReaped(-1)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "ws_agent_trace_stale_reaped_total" {
			if len(family.Metric) != 1 || family.Metric[0].Counter.GetValue() < 3 {
				t.Fatalf("unexpected stale trace metric: %#v", family.Metric)
			}
			return
		}
	}
	t.Fatal("stale trace metric missing")
}
