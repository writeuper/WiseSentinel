package observability

import "testing"

func TestAuditWriteFailureUsesBoundedReasons(t *testing.T) {
	ObserveAuditWriteFailure("context_canceled")
	ObserveAuditWriteFailure("database")
	ObserveAuditWriteFailure("raw sql error with secret")
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, family := range families {
		if family.GetName() != "ws_audit_write_failures_total" {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != 1 || metric.Label[0].GetName() != "reason" {
				t.Fatalf("unexpected audit labels: %#v", metric.Label)
			}
			reason := metric.Label[0].GetValue()
			if reason != "context_canceled" && reason != "database" {
				t.Fatalf("unbounded audit reason %q", reason)
			}
			seen[reason] = metric.Counter.GetValue() >= 1
		}
	}
	if !seen["context_canceled"] || !seen["database"] {
		t.Fatalf("audit failure reasons missing: %#v", seen)
	}
}
