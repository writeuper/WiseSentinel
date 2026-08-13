package ops

import (
	"context"
	"errors"
	"testing"

	"wisesentinel-platform/internal/pkg/apperr"
)

func TestOpsAgentRejectsNilRequest(t *testing.T) {
	agent := NewAgent(nil, nil, nil, nil)
	if _, err := agent.Analyze(context.Background(), nil); !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("Analyze(nil) error = %v, want bad request", err)
	}
}
