package middleware

import (
	"errors"
	"testing"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"wisesentinel-platform/internal/pkg/apperr"
)

func TestIsValidationErrorRecognizesGoFrameValidationAndRejectsOtherErrors(t *testing.T) {
	validation := gerror.NewCode(gcode.CodeValidationFailed, "The Question field is required")
	if !isValidationError(validation) {
		t.Fatal("GoFrame validation error was not recognized")
	}
	if isValidationError(errors.New("provider secret=must-not-leak")) {
		t.Fatal("arbitrary provider error was recognized as validation")
	}
}

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
