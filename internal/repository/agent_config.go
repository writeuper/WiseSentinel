package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"wisesentinel-platform/internal/domain"

	"github.com/gogf/gf/v2/frame/g"
)

// AgentConfigRepo reads immutable, tenant-scoped configuration versions.
// Activation remains owned by the handler transaction; this repository only
// supplies a validated runtime snapshot to Agent executions.
type AgentConfigRepo struct{}

func NewAgentConfigRepo() *AgentConfigRepo { return &AgentConfigRepo{} }

// ValidateAgentConfigJSON performs activation-time schema validation. An
// invalid version must never become active and strand requests after a
// successful pointer switch.
func ValidateAgentConfigJSON(agentType, version, rawJSON string) error {
	if strings.TrimSpace(version) == "" || strings.TrimSpace(agentType) == "" {
		return fmt.Errorf("agent type and version are required")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil || raw == nil {
		return fmt.Errorf("config_json must be a JSON object")
	}
	if prompt, ok := raw["system_prompt"]; ok {
		var value string
		if err := json.Unmarshal(prompt, &value); err != nil || utf8.RuneCountInString(value) > 16000 {
			return fmt.Errorf("system_prompt is invalid or too long")
		}
	}
	if iterations, ok := raw["max_iterations"]; ok {
		var value int
		if err := json.Unmarshal(iterations, &value); err != nil || value < 0 || value > 100 {
			return fmt.Errorf("max_iterations must be between 0 and 100")
		}
	}
	if tools, ok := raw["tools"]; ok {
		var values []string
		if err := json.Unmarshal(tools, &values); err != nil || len(values) > 64 {
			return fmt.Errorf("tools must be a bounded string list")
		}
		seen := make(map[string]struct{}, len(values))
		for _, tool := range values {
			tool = strings.TrimSpace(tool)
			if tool == "" || len(tool) > 128 || strings.ContainsAny(tool, "\r\n") {
				return fmt.Errorf("tools contains an invalid name")
			}
			if _, exists := seen[tool]; exists {
				return fmt.Errorf("tools contains duplicate name")
			}
			seen[tool] = struct{}{}
		}
	}
	if agentType == string(domain.AgentTypeKnowledge) {
		if chunk, ok := raw["chunk_size"]; ok {
			var value int
			if err := json.Unmarshal(chunk, &value); err != nil || value < 100 || value > 2000 {
				return fmt.Errorf("chunk_size must be between 100 and 2000")
			}
		}
		if overlap, ok := raw["overlap"]; ok {
			var value int
			if err := json.Unmarshal(overlap, &value); err != nil || value < 0 || value > 500 {
				return fmt.Errorf("overlap must be between 0 and 500")
			}
		}
	}
	return nil
}

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
