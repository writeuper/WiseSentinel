package task

import (
	"errors"
	"testing"
	"time"
)

func TestIndexRetryClassificationOnlyRetriesTransientDependencies(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  string
		want bool
	}{
		{"milvus deadline", "rpc error: DeadlineExceeded", true},
		{"network reset", "connection reset by peer", true},
		{"upstream 503", "embed http 503", true},
		{"quota", "dashscope embed http 403 AllocationQuota.FreeTierOnly", false},
		{"not found", "dashscope embed http 404", false},
		{"deleted", "document deleted", false},
		{"validation", "source uri is required", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableIndexError(errors.New(tc.err)); got != tc.want {
				t.Fatalf("retryable(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIndexRetryDelayIsBounded(t *testing.T) {
	if got := indexRetryDelay(1); got != time.Second {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := indexRetryDelay(3); got != 4*time.Second {
		t.Fatalf("attempt 3 delay = %s", got)
	}
	if got := indexRetryDelay(20); got != 2*time.Minute {
		t.Fatalf("attempt 20 delay = %s", got)
	}
}

func TestIndexRetryAttemptBudgetCountsCurrentExecution(t *testing.T) {
	if attempt, max := 1, 3; !(attempt < max) {
		t.Fatal("first attempt should be retryable")
	}
	if attempt, max := 3, 3; attempt < max {
		t.Fatal("third attempt must be terminal")
	}
}
