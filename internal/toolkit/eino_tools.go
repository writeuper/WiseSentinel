package toolkit

import (
	"context"
	"encoding/json"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/redact"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// einoToolWrapper wraps a ToolGateway invocation as a tool.InvokableTool.
type einoToolWrapper struct {
	gateway   *Gateway
	meta      *domain.ToolMeta
	tenantID  string
	agentType domain.AgentType
}

func (w *einoToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{
		"input": {
			Type:     schema.String,
			Desc:     "Tool arguments as a JSON object. Do not omit required fields.",
			Required: false,
		},
	}
	if w.meta.Name == "query_logs" || w.meta.Name == "search_logs" {
		params["input"] = &schema.ParameterInfo{
			Type:     schema.String,
			Desc:     `JSON object, for example {"query":"order-service 500","service":"order-service","limit":10}; query is required and must not be empty.`,
			Required: true,
		}
	}
	return &schema.ToolInfo{
		Name:        w.meta.Name,
		Desc:        w.meta.Description,
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

func (w *einoToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	// #region debug-point A:tool-wrapper-start
	g.Log().Infof(ctx, "[DEBUG] Eino tool invoke start trace=%s tool=%s agent=%s args=%s", traceID, w.meta.Name, w.agentType, redact.TelemetryProjection(argumentsInJSON))
	// #endregion
	resp, err := w.gateway.Invoke(ctx, &domain.ToolInvokeRequest{
		TenantID:  w.tenantID,
		UserID:    ctxkeys.UserIDFrom(ctx),
		TraceID:   traceID,
		ToolName:  w.meta.Name,
		Input:     json.RawMessage(argumentsInJSON),
		AgentType: w.agentType,
	})
	if err != nil {
		if resp != nil && resp.Output != "" {
			return resp.Output, err
		}
		return "", err
	}
	return resp.Output, nil
}

// AsEinoTools converts the gateway's enabled tools for an agent type into Eino BaseTool slice.
func (gw *Gateway) AsEinoTools(ctx context.Context, tenantID string, agentType domain.AgentType) ([]tool.BaseTool, error) {
	toolMetas, err := gw.ListTools(ctx, tenantID, agentType)
	if err != nil {
		return nil, err
	}

	einoTools := make([]tool.BaseTool, 0, len(toolMetas))
	for _, meta := range toolMetas {
		einoTools = append(einoTools, &einoToolWrapper{
			gateway:   gw,
			meta:      &meta,
			tenantID:  tenantID,
			agentType: agentType,
		})
	}
	return einoTools, nil
}

// Ensure einoToolWrapper implements tool.InvokableTool.
var _ tool.InvokableTool = (*einoToolWrapper)(nil)
