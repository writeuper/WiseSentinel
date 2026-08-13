package task

import (
	"testing"
	"time"
)

func TestAlertEventReaperDefaultsAreBounded(t *testing.T) {
	if alertEventReapAge != 5*time.Minute {
		t.Fatalf("reap age = %s, want 5m", alertEventReapAge)
	}
	if alertEventReapInterval != time.Minute {
		t.Fatalf("reap interval = %s, want 1m", alertEventReapInterval)
	}
}
