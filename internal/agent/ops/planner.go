// Package ops implements the Ops Agent using Eino v0.6.0's Plan-Execute-Replan pattern.
package ops

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/domain"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	einomod "github.com/cloudwego/eino/components/model"
)

// NewPlanner creates the Planner agent for Ops.
//
// The Planner uses the ops_plan (think) model and produces a structured
// Plan containing the steps needed to satisfy the user's query.
func NewPlanner(ctx context.Context, modelRouter domain.ModelRouter) (adk.Agent, error) {
	rawModel, err := modelRouter.ChatModel(ctx, domain.ModelProfileOpsPlan)
	if err != nil {
		return nil, fmt.Errorf("get ops_plan model: %w", err)
	}
	planModel, ok := rawModel.(einomod.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("ops_plan model does not implement model.ToolCallingChatModel")
	}
	return planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: planModel,
	})
}
