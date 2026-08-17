package ops

import (
	"context"
	"errors"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
)

func TestOpsAgentRejectsNilRequest(t *testing.T) {
	agent := NewAgent(nil, nil, nil, nil)
	if _, err := agent.Analyze(context.Background(), nil); !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("Analyze(nil) error = %v, want bad request", err)
	}
}

func TestEffectiveMaxIterationsUsesStrictestConfiguredBound(t *testing.T) {
	if got := effectiveMaxIterations(20, &domain.OpsAgentRequest{MaxIterations: 7}); got != 7 {
		t.Fatalf("request budget = %d, want 7", got)
	}
	if got := effectiveMaxIterations(20, &domain.OpsAgentRequest{MaxIterations: 12, RuntimeConfig: &domain.AgentRuntimeConfig{MaxIterations: 5}}); got != 5 {
		t.Fatalf("runtime budget = %d, want 5", got)
	}
}

func TestCancellationSafePersistenceContextRetainsTraceValues(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), "trace", "trace-1"))
	cancel()
	persist := context.WithoutCancel(parent)
	if persist.Err() != nil || persist.Value("trace") != "trace-1" {
		t.Fatalf("persistence context err/value = %v/%v", persist.Err(), persist.Value("trace"))
	}
}
