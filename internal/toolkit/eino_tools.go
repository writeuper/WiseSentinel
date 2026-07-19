package toolkit

import (
	"context"
	"encoding/json"

	"wisesentinel-platform/internal/domain"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// einoToolWrapper wraps a ToolGateway invocation as a tool.InvokableTool.
type einoToolWrapper struct {
	gateway   *Gateway
	meta      *domain.ToolMeta
	tenantID  string
	agentType domain.AgentType
}

func (w *einoToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: w.meta.Name,
		Desc: w.meta.Description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"input": {
				Type:     schema.String,
				Desc:     "The input parameters for the tool in JSON format",
				Required: false,
			},
		}),
	}, nil
}

func (w *einoToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	resp, err := w.gateway.Invoke(ctx, &domain.ToolInvokeRequest{
		TenantID:  w.tenantID,
		ToolName:  w.meta.Name,
		Input:     json.RawMessage(argumentsInJSON),
		AgentType: w.agentType,
	})
	if err != nil {
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
