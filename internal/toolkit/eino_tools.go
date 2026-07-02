package toolkit

import (
	"context"
	"encoding/json"

	"wisesentinel-platform/internal/domain"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// einoToolWrapper wraps a tool adapter as a tool.InvokableTool.
type einoToolWrapper struct {
	meta    *domain.ToolMeta
	adapter AdapterFunc
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
	input := json.RawMessage(argumentsInJSON)
	return w.adapter(ctx, input)
}

// AsEinoTools converts the gateway's enabled tools for an agent type into Eino BaseTool slice.
func (gw *Gateway) AsEinoTools(ctx context.Context, tenantID string, agentType domain.AgentType) ([]tool.BaseTool, error) {
	toolMetas, err := gw.ListTools(ctx, tenantID, agentType)
	if err != nil {
		return nil, err
	}

	einoTools := make([]tool.BaseTool, 0, len(toolMetas))
	for _, meta := range toolMetas {
		adapter, ok := gw.adapters[meta.Name]
		if !ok {
			continue
		}
		einoTools = append(einoTools, &einoToolWrapper{
			meta:    &meta,
			adapter: adapter,
		})
	}
	return einoTools, nil
}

// Ensure einoToolWrapper implements tool.InvokableTool.
var _ tool.InvokableTool = (*einoToolWrapper)(nil)