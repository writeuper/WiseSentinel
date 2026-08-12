package middleware

import (
	"testing"

	"wisesentinel-platform/internal/pkg/apperr"
)

func TestRetryAfterSecondsOnlyMarksExplicitlyRetryableOverload(t *testing.T) {
	if got := retryAfterSeconds(apperr.ErrModelOverloaded.Code); got != 2 {
		t.Fatalf("model overload retry-after = %d, want 2", got)
	}
	for _, code := range []int{apperr.ErrModelTimeout.Code, apperr.ErrAgentFailed.Code, apperr.ErrHighRiskWorkflowUnavailable.Code} {
		if got := retryAfterSeconds(code); got != 0 {
			t.Fatalf("retry-after for code %d = %d, want 0", code, got)
		}
	}
}
