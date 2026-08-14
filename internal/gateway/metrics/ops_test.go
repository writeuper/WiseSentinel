package metrics

import "testing"

func TestNormalizeOpsMetricLabels(t *testing.T) {
	if got := normalizeOpsStage("e2e"); got != "e2e" {
		t.Fatalf("known stage = %q", got)
	}
	if got := normalizeOpsStage("planner-injected"); got != "other" {
		t.Fatalf("unknown stage = %q, want other", got)
	}
	if got := normalizeOpsStatus("success"); got != "success" {
		t.Fatalf("known status = %q", got)
	}
	if got := normalizeOpsStatus("tenant-secret-status"); got != "other" {
		t.Fatalf("unknown status = %q, want other", got)
	}
}

func TestOpsTaskReaperMetricsUseBoundedOutcomeAndMonotonicTimeouts(t *testing.T) {
	ObserveOpsTaskReaperRun("success")
	ObserveOpsTaskReaperRun("database-password-secret")
	ObserveOpsTaskTimeouts(2)
	ObserveOpsTaskTimeouts(-1)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var sawTimeout, sawSuccess, sawOther bool
	for _, family := range families {
		for _, metric := range family.Metric {
			switch family.GetName() {
			case "ws_ops_task_timeouts_total":
				sawTimeout = metric.Counter.GetValue() >= 2
			case "ws_ops_task_reaper_runs_total":
				for _, label := range metric.Label {
					if label.GetName() == "outcome" && label.GetValue() == "success" {
						sawSuccess = metric.Counter.GetValue() >= 1
					}
					if label.GetName() == "outcome" && label.GetValue() == "other" {
						sawOther = metric.Counter.GetValue() >= 1
					}
				}
			}
		}
	}
	if !sawTimeout || !sawSuccess || !sawOther {
		t.Fatalf("reaper metrics missing: timeout=%t success=%t other=%t", sawTimeout, sawSuccess, sawOther)
	}
}

func TestNormalizeReaperOutcome(t *testing.T) {
	if normalizeReaperOutcome("error") != "error" || normalizeReaperOutcome("tenant-secret") != "other" {
		t.Fatal("reaper outcome label is not bounded")
	}
}
