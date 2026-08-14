package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"wisesentinel-platform/internal/domain"

	"github.com/gogf/gf/v2/frame/g"
)

// AgentConfigRepo reads immutable, tenant-scoped configuration versions.
// Activation remains owned by the handler transaction; this repository only
// supplies a validated runtime snapshot to Agent executions.
type AgentConfigRepo struct{}

func NewAgentConfigRepo() *AgentConfigRepo { return &AgentConfigRepo{} }

type agentConfigRow struct {
	TenantID   string `json:"tenant_id"`
	AgentType  string `json:"agent_type"`
	Version    string `json:"version"`
	ConfigJSON string `json:"config_json"`
}

func (r *AgentConfigRepo) GetActiveAgentConfig(ctx context.Context, tenantID string, agentType domain.AgentType) (*domain.AgentRuntimeConfig, error) {
	var row agentConfigRow
	err := g.DB().Model("ws_agent_config").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("agent_type", string(agentType)).
		Where("is_active", 1).
		OrderDesc("created_at").
		Limit(1).
		Scan(&row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.Version == "" {
		return nil, nil
	}
	return decodeAgentConfig(row)
}

func (r *AgentConfigRepo) GetAgentConfig(ctx context.Context, tenantID string, agentType domain.AgentType, version string) (*domain.AgentRuntimeConfig, error) {
	version = strings.TrimSpace(version)
	if tenantID == "" || version == "" {
		return nil, nil
	}
	var row agentConfigRow
	err := g.DB().Model("ws_agent_config").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("agent_type", string(agentType)).
		Where("version", version).
		Limit(1).
		Scan(&row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.Version == "" {
		return nil, nil
	}
	return decodeAgentConfig(row)
}

func decodeAgentConfig(row agentConfigRow) (*domain.AgentRuntimeConfig, error) {
	var raw struct {
		SystemPrompt  string   `json:"system_prompt"`
		MaxIterations int      `json:"max_iterations"`
		Tools         []string `json:"tools"`
	}
	if err := json.Unmarshal([]byte(row.ConfigJSON), &raw); err != nil {
		return nil, fmt.Errorf("invalid agent config %s/%s: %w", row.AgentType, row.Version, err)
	}
	if raw.MaxIterations < 0 || raw.MaxIterations > 100 {
		return nil, fmt.Errorf("agent config %s/%s max_iterations out of range", row.AgentType, row.Version)
	}
	tools := make([]string, 0, len(raw.Tools))
	seen := make(map[string]struct{}, len(raw.Tools))
	for _, name := range raw.Tools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tools = append(tools, name)
	}
	return &domain.AgentRuntimeConfig{
		AgentType:     domain.AgentType(row.AgentType),
		Version:       row.Version,
		SystemPrompt:  strings.TrimSpace(raw.SystemPrompt),
		MaxIterations: raw.MaxIterations,
		Tools:         tools,
	}, nil
}

var _ domain.AgentConfigProvider = (*AgentConfigRepo)(nil)
