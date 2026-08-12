package bootstrap

import (
	"context"
	"testing"
	"time"
)

func TestMCPTimeInitTimeoutUsesBoundedConfiguration(t *testing.T) {
	t.Setenv("MCP_TIME_INIT_TIMEOUT_MS", "1500")
	if got := mcpTimeInitTimeout(context.Background()); got != 1500*time.Millisecond {
		t.Fatalf("mcpTimeInitTimeout() = %s, want 1.5s", got)
	}
}

func TestMCPTimeInitTimeoutFallsBackForUnsafeConfiguration(t *testing.T) {
	for _, value := range []string{"invalid", "249", "10001"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MCP_TIME_INIT_TIMEOUT_MS", value)
			if got := mcpTimeInitTimeout(context.Background()); got != defaultMCPTimeInitTimeout {
				t.Fatalf("mcpTimeInitTimeout() = %s, want safe default %s", got, defaultMCPTimeInitTimeout)
			}
		})
	}
}

func TestReadinessProbeTimeoutIsBounded(t *testing.T) {
	if readinessProbeTimeout <= 0 || readinessProbeTimeout > 5*time.Second {
		t.Fatalf("readinessProbeTimeout = %s, want a positive rollout-safe budget no greater than 5s", readinessProbeTimeout)
	}
}
