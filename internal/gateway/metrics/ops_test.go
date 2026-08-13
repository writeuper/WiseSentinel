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
