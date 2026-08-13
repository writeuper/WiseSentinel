package observability

import "testing"

func TestObserveToolCallUsesBoundedLabels(t *testing.T) {
	ObserveToolCall("tool\nwith-secret", "attacker-agent", "raw provider error", 0.01)
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_tool_calls_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetValue() == "tool\nwith-secret" || label.GetValue() == "attacker-agent" || label.GetValue() == "raw provider error" {
					t.Fatal("unbounded tool label exported")
				}
			}
		}
	}
}
