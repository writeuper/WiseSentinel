package ops

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/domain"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
)

// NewReplanner creates the Replanner agent for Ops.
//
// The Replanner uses the ops_plan (think) model and decides whether
// the current Plan is sufficient (call the Respond tool to end the loop)
// or needs another iteration (call the Plan tool to refine).
func NewReplanner(ctx context.Context, modelRouter domain.ModelRouter) (adk.Agent, error) {
	rawModel, err := modelRouter.ChatModel(ctx, domain.ModelProfileOpsPlan)
	if err != nil {
		return nil, fmt.Errorf("get ops_plan model for replanner: %w", err)
	}
	replanModel, ok := rawModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("ops_plan model does not implement model.ToolCallingChatModel")
	}
	return planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: newForceToolModel(replanModel),
	})
}
