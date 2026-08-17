package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// taskCompleteTool is an internal control-plane action. It has no external
// effect; it only records the model's proposed completion for runtime review.
type taskCompleteTool struct{}

func (taskCompleteTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "task_complete", Desc: "Submit an explicit completion only after required evidence has been collected.", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"status":    {Type: schema.String, Required: true, Desc: "must be completed"},
		"summary":   {Type: schema.String, Required: true, Desc: "evidence-based conclusion"},
		"checklist": {Type: schema.Array, Required: true, Desc: "items with item and completed fields"},
	})}, nil
}

func (taskCompleteTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	var proposal domain.TaskCompletion
	if err := json.Unmarshal([]byte(args), &proposal); err != nil {
		return "", fmt.Errorf("invalid task_complete payload: %w", err)
	}
	if strings.TrimSpace(proposal.Status) != "completed" {
		return "", fmt.Errorf("task_complete status must be completed")
	}
	sink := ctxkeys.TaskCompletionSinkFrom(ctx)
	if sink == nil {
		return "", fmt.Errorf("task completion is not enabled for this request")
	}
	*sink = proposal
	return `{"accepted_for_runtime_validation":true}`, nil
}

var _ tool.InvokableTool = taskCompleteTool{}
