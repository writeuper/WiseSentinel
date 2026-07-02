// Package toolkit implements the ToolGateway for agent tool invocation.
package toolkit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/toolkit/adapters"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/gconv"
)

// AdapterFunc is the signature for a tool adapter.
type AdapterFunc func(ctx context.Context, input json.RawMessage) (string, error)

// Gateway implements domain.ToolGateway.
type Gateway struct {
	mu       sync.RWMutex
	tools    map[string]*domain.ToolMeta
	adapters map[string]AdapterFunc
}

// NewGateway creates a ToolGateway from config and registered adapters.
func NewGateway(ctx context.Context) *Gateway {
	gw := &Gateway{
		tools:    make(map[string]*domain.ToolMeta),
		adapters: make(map[string]AdapterFunc),
	}
	gw.loadToolConfig(ctx)
	gw.registerAdapters()
	return gw
}

func (gw *Gateway) loadToolConfig(ctx context.Context) {
	tools := g.Cfg().MustGet(ctx, "tools").Slice()
	for _, raw := range tools {
		toolMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name := gconv.String(toolMap["name"])
		if name == "" {
			continue
		}
		enabled := gconv.Bool(toolMap["enabled"])
		agentsRaw := toolMap["agents"]
		agents := make([]domain.AgentType, 0)
		if agentsSlice, ok := agentsRaw.([]interface{}); ok {
			for _, a := range agentsSlice {
				agents = append(agents, domain.AgentType(gconv.String(a)))
			}
		}

		riskLevel := gconv.String(toolMap["risk_level"])
		var rl domain.ToolRiskLevel
		switch riskLevel {
		case "L0":
			rl = domain.ToolRiskL0Readonly
		case "L1":
			rl = domain.ToolRiskL1SensitiveRead
		case "L2":
			rl = domain.ToolRiskL2Write
		default:
			rl = domain.ToolRiskL0Readonly
		}

		gw.tools[name] = &domain.ToolMeta{
			Name:        name,
			Description: gconv.String(toolMap["description"]),
			RiskLevel:   rl,
			TimeoutMS:   gconv.Int(toolMap["timeout_ms"]),
			Agents:      agents,
			Enabled:     enabled,
		}
	}
}

func (gw *Gateway) registerAdapters() {
	gw.adapters["query_prometheus_alerts"] = adapters.QueryPrometheusAlerts
	gw.adapters["query_internal_docs"] = adapters.QueryInternalDocs
	gw.adapters["get_current_time"] = adapters.GetCurrentTime
	gw.adapters["query_logs"] = adapters.NewQueryLogs()
}

// ListTools returns tools enabled for a given agent type.
func (gw *Gateway) ListTools(ctx context.Context, tenantID string, agentType domain.AgentType) ([]domain.ToolMeta, error) {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	var result []domain.ToolMeta
	for _, meta := range gw.tools {
		if !meta.Enabled {
			continue
		}
		if !agentInList(agentType, meta.Agents) {
			continue
		}
		result = append(result, *meta)
	}
	return result, nil
}

// Invoke calls a tool adapter with timeout and risk checks.
func (gw *Gateway) Invoke(ctx context.Context, req *domain.ToolInvokeRequest) (*domain.ToolInvokeResponse, error) {
	gw.mu.RLock()
	meta, ok := gw.tools[req.ToolName]
	adapter, hasAdapter := gw.adapters[req.ToolName]
	gw.mu.RUnlock()

	if !ok || !meta.Enabled {
		return nil, apperr.New(50003, 500, fmt.Sprintf("tool %q is not available", req.ToolName))
	}
	if !hasAdapter {
		return nil, apperr.New(50003, 500, fmt.Sprintf("tool %q has no adapter", req.ToolName))
	}

	// RBAC check based on risk level
	if err := gw.checkRiskLevel(ctx, meta.RiskLevel); err != nil {
		return nil, err
	}

	start := time.Now()
	timeout := time.Duration(meta.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := adapter(callCtx, req.Input)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &domain.ToolInvokeResponse{
			Output:    fmt.Sprintf("tool error: %v", err),
			Status:    "error",
			LatencyMS: int(latency),
		}, nil
	}
	return &domain.ToolInvokeResponse{
		Output:    output,
		Status:    "success",
		LatencyMS: int(latency),
	}, nil
}

func (gw *Gateway) checkRiskLevel(ctx context.Context, riskLevel domain.ToolRiskLevel) error {
	roles := ctxkeys.RolesFrom(ctx)
	switch riskLevel {
	case domain.ToolRiskL0Readonly:
		return nil // any authenticated user
	case domain.ToolRiskL1SensitiveRead:
		// operator+ required
		for _, role := range roles {
			switch domain.Role(role) {
			case domain.RoleOperator, domain.RoleSREAdmin, domain.RolePlatformAdmin:
				return nil
			}
		}
		return apperr.ErrForbidden
	case domain.ToolRiskL2Write:
		// sre_admin+ required
		for _, role := range roles {
			switch domain.Role(role) {
			case domain.RoleSREAdmin, domain.RolePlatformAdmin:
				return nil
			}
		}
		return apperr.ErrForbidden
	default:
		return nil
	}
}

func agentInList(agent domain.AgentType, list []domain.AgentType) bool {
	for _, a := range list {
		if a == agent {
			return true
		}
	}
	return false
}

// Ensure Gateway implements domain.ToolGateway.
var _ domain.ToolGateway = (*Gateway)(nil)