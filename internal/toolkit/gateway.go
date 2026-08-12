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
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/repository"
	"wisesentinel-platform/internal/toolkit/adapters"
	mcpadapter "wisesentinel-platform/internal/toolkit/mcp"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/gconv"
)

// AdapterFunc is the signature for a tool adapter.
type AdapterFunc func(ctx context.Context, input json.RawMessage) (string, error)

// Gateway implements domain.ToolGateway.
type Gateway struct {
	mu         sync.RWMutex
	tools      map[string]*domain.ToolMeta
	adapters   map[string]AdapterFunc
	recordRepo *repository.ToolCallRecordRepo
}

// NewGateway creates a ToolGateway from config and registered adapters.
func NewGateway(ctx context.Context) *Gateway {
	gw := &Gateway{
		tools:    make(map[string]*domain.ToolMeta),
		adapters: make(map[string]AdapterFunc),
	}
	gw.loadToolConfig(ctx)
	gw.registerAdapters()

	// The approval repo is set via SetApprovalRepo after bootstrap.
	return gw
}

func (gw *Gateway) SetToolCallRecordRepo(repo *repository.ToolCallRecordRepo) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.recordRepo = repo
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

func (gw *Gateway) SetMCPTimeAdapter(adapter *mcpadapter.TimeAdapter) {
	if adapter == nil {
		return
	}
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.adapters["mcp_time_get_current_time"] = adapter.GetCurrentTime
	gw.adapters["mcp_time_convert_time"] = adapter.ConvertTime
}

// EnableInternalDocsAdapter exposes knowledge retrieval only after bootstrap
// has created a usable RAG pipeline. Keeping an unavailable dependency in the
// model tool list causes tool-selection failures to become user-visible Chat
// 500s instead of an explicit degraded capability.
func (gw *Gateway) EnableInternalDocsAdapter() {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.adapters["query_internal_docs"] = adapters.QueryInternalDocs
}

func (gw *Gateway) registerAdapters() {
	gw.adapters["query_prometheus_alerts"] = adapters.QueryPrometheusAlerts
	gw.adapters["query_metric_range"] = adapters.QueryMetricRange
	gw.adapters["get_current_time"] = adapters.GetCurrentTime
	// Keep the MCP-compatible time tool names available during optional MCP
	// degradation. Bootstrap replaces these adapters with the real MCP client
	// when it starts successfully.
	gw.adapters["mcp_time_get_current_time"] = adapters.GetCurrentTime
	gw.adapters["mcp_time_convert_time"] = adapters.ConvertTime
	gw.adapters["query_logs"] = adapters.NewQueryLogs()
	gw.adapters["search_logs"] = adapters.NewSearchLogs()
	gw.adapters["query_logs_by_trace"] = adapters.NewQueryLogsByTrace()
	gw.adapters["query_deployments"] = adapters.NewQueryDeployments()
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
		if _, ok := gw.adapters[meta.Name]; !ok {
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
	recordRepo := gw.recordRepo
	gw.mu.RUnlock()

	if !ok || !meta.Enabled {
		return nil, apperr.New(50003, 500, fmt.Sprintf("tool %q is not available", req.ToolName))
	}
	if !hasAdapter {
		return nil, apperr.New(50003, 500, fmt.Sprintf("tool %q has no adapter", req.ToolName))
	}

	// L2 writes require a durable execution intent: an immutable, protected
	// parameter snapshot, approval/task binding, SoD, outbox and idempotent
	// executor fencing. The current platform has no configured L2 adapter or
	// such executor. Creating an approval here would be deceptive: approval
	// could succeed while the original action can never safely resume. Refuse
	// all L2 calls until that workflow is available (including admins, so an
	// admin role cannot bypass the missing evidence/approval boundary).
	if meta.RiskLevel == domain.ToolRiskL2Write {
		return nil, apperr.ErrHighRiskWorkflowUnavailable
	} else if err := gw.checkRiskLevel(ctx, meta.RiskLevel); err != nil {
		return nil, err
	}

	req.Input = completeToolInput(req.ToolName, req.Input, ctx)
	// region debug-point p1-tool-input
	g.Log().Infof(ctx, "[p1-tool-input] tool=%s agent=%s input=%s", req.ToolName, req.AgentType, redact.TelemetryProjection(string(req.Input)))
	// endregion debug-point p1-tool-input
	// #region debug-point A:tool-invoke-start
	g.Log().Infof(ctx, "[DEBUG] tool invoke start trace=%s tool=%s agent=%s timeout_ms=%d", req.TraceID, req.ToolName, req.AgentType, meta.TimeoutMS)
	// #endregion
	start := time.Now()
	timeout := time.Duration(meta.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := adapter(callCtx, req.Input)
	latency := time.Since(start).Milliseconds()
	var (
		respStatus = "success"
		respOutput = output
	)
	if err != nil {
		// region debug-point p1-p2-tool-error
		g.Log().Warningf(ctx, "[p1-p2-tool-error] trace=%s tool=%s agent=%s status=error cause=%s", req.TraceID, req.ToolName, req.AgentType, redact.TelemetryProjection(err.Error()))
		// endregion debug-point p1-p2-tool-error
		respStatus = "error"
		respOutput = "tool error: " + diagnosticSummary(err.Error(), 1000)
	}
	// Record evidence when the caller attached a tool sink to the context.
	// This is how the Ops Agent builds a structured proof chain without
	// touching the executor or the shared gateway state.
	inputSummary := redact.TelemetryProjection(string(req.Input))
	outputSummary := redact.TelemetryProjection(respOutput)
	if sink := ctxkeys.ToolSinkFrom(ctx); sink != nil {
		*sink = append(*sink, domain.Evidence{
			ToolName:  req.ToolName,
			Input:     inputSummary,
			Output:    outputSummary,
			Status:    respStatus,
			LatencyMS: latency,
			Timestamp: time.Now().UTC().Format("2006-01-02 15:04:05"),
		})
	}
	// #region debug-point A:tool-invoke-result
	g.Log().Infof(ctx, "[DEBUG] tool invoke result trace=%s tool=%s agent=%s status=%s latency_ms=%d output=%s", req.TraceID, req.ToolName, req.AgentType, respStatus, latency, redact.TelemetryProjection(respOutput))
	// #endregion
	if sink := ctxkeys.StepSinkFrom(ctx); sink != nil {
		errMsg := ""
		if err != nil {
			errMsg = redact.TelemetryProjection(err.Error())
		}
		sink("tool", req.ToolName, inputSummary, outputSummary, respStatus, latency, errMsg)
	}
	if recordRepo != nil {
		tenantID := req.TenantID
		if tenantID == "" {
			tenantID = ctxkeys.TenantIDFrom(ctx)
		}
		_ = recordRepo.Create(ctx, &repository.ToolCallRecord{
			TenantID:  tenantID,
			TraceID:   req.TraceID,
			ToolName:  req.ToolName,
			AgentType: string(req.AgentType),
			Input:     inputSummary,
			Output:    outputSummary,
			Status:    respStatus,
			LatencyMS: latency,
		})
	}
	response := &domain.ToolInvokeResponse{
		Output:    respOutput,
		Status:    respStatus,
		LatencyMS: int(latency),
	}
	if err != nil {
		return response, err
	}
	return response, nil
}

// truncateJSON keeps evidence payloads small enough to fit in an LLM context
// and in the detail_json column.
func truncateJSON(s string, maxLen int) string {
	return diagnosticSummary(s, maxLen)
}

// diagnosticSummary is deliberately used only for logs, trace steps and
// durable diagnostic records. Tool adapters and LLM execution still receive
// the original request and successful response values.
func diagnosticSummary(s string, maxLen int) string {
	return redact.Summary(redact.JSON(s), maxLen)
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
