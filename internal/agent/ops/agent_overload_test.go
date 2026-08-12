package ops

import (
	"errors"
	"testing"

	"wisesentinel-platform/internal/pkg/apperr"
)

func TestIsModelOverloadedErrorRecognizesTypedAndGraphFormattedErrors(t *testing.T) {
	if !isModelOverloadedError(apperr.ErrModelOverloaded) {
		t.Fatal("typed model overload was not recognized")
	}
	if !isModelOverloadedError(errors.New("[NodeRunError] " + apperr.ErrModelOverloaded.Message)) {
		t.Fatal("graph-formatted model overload was not recognized")
	}
	if isModelOverloadedError(errors.New("unrelated model failure")) {
		t.Fatal("unrelated error was recognized as model overload")
	}
}

func TestNormalizeOpsEventErrorPreservesOverloadAsStableAppError(t *testing.T) {
	if got := normalizeOpsEventError(errors.New("[NodeRunError] " + apperr.ErrModelOverloaded.Message)); got != apperr.ErrModelOverloaded {
		t.Fatalf("normalized overload = %v, want stable AppError", got)
	}
	original := errors.New("generic graph failure")
	if got := normalizeOpsEventError(original); got != original {
		t.Fatal("generic graph error was changed")
	}
}
