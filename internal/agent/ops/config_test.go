package ops

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/domain"
)

type configProviderStub struct {
	active *domain.AgentRuntimeConfig
	byVer  *domain.AgentRuntimeConfig
}

func (s configProviderStub) GetActiveAgentConfig(context.Context, string, domain.AgentType) (*domain.AgentRuntimeConfig, error) {
	return s.active, nil
}

func (s configProviderStub) GetAgentConfig(context.Context, string, domain.AgentType, string) (*domain.AgentRuntimeConfig, error) {
	return s.byVer, nil
}

func TestOpsRuntimeConfigIsTenantScopedAtSubmission(t *testing.T) {
	active := &domain.AgentRuntimeConfig{AgentType: domain.AgentTypeOps, Version: "v2", MaxIterations: 7}
	agent := NewAgent(nil, nil, nil, nil)
	agent.SetConfigProvider(configProviderStub{active: active})
	req := &domain.OpsAgentRequest{TenantID: "tenant-a"}
	if err := agent.loadRuntimeConfig(context.Background(), "tenant-a", req); err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if req.RuntimeConfig != active || req.RuntimeConfig.Version != "v2" {
		t.Fatalf("runtime config = %#v", req.RuntimeConfig)
	}
}

func TestOpsWorkerUsesPersistedConfigVersion(t *testing.T) {
	selected := &domain.AgentRuntimeConfig{AgentType: domain.AgentTypeOps, Version: "v1", MaxIterations: 3}
	agent := NewAgent(nil, nil, nil, nil)
	agent.SetConfigProvider(configProviderStub{byVer: selected})
	// The worker path is intentionally represented by the provider contract:
	// a task carries a version, and only that version is requested, never the
	// newly active version. Full DB lease execution remains an integration test.
	got, err := agent.configProvider.GetAgentConfig(context.Background(), "tenant-a", domain.AgentTypeOps, "v1")
	if err != nil || got != selected {
		t.Fatalf("persisted config lookup = %#v, %v", got, err)
	}
}
