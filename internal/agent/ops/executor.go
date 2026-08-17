package ops

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/toolkit"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
)

// NewExecutor creates the Executor agent for Ops.
//
// The Executor uses the ops_exec (quick) model and the Eino-wrapped
// toolset exposed by ToolGateway. It walks the Plan step by step and
// invokes tools as the model requests.
func NewExecutor(ctx context.Context, modelRouter domain.ModelRouter, toolGateway domain.ToolGateway, tenantID string) (adk.Agent, error) {
	// 1. Get the ops_exec chat model
	rawModel, err := modelRouter.ChatModel(ctx, domain.ModelProfileOpsExec)
	if err != nil {
		return nil, fmt.Errorf("get ops_exec model: %w", err)
	}
	execModel, ok := rawModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("ops_exec model does not implement model.ToolCallingChatModel")
	}

	// 2. Get the Eino-compatible tool list for ops agents
	//    This reuses the same adapters as the Chat Agent.
	gw, ok := toolGateway.(*toolkit.Gateway)
	if !ok {
		return nil, fmt.Errorf("toolGateway is not a *toolkit.Gateway")
	}
	einoTools, err := gw.AsEinoTools(ctx, tenantID, domain.AgentTypeOps)
	if err != nil {
		return nil, fmt.Errorf("list ops tools: %w", err)
	}
	// Completion is a runtime control-plane tool, not a Gateway adapter: it
	// cannot cause an external side effect and is reviewed after the graph ends.
	einoTools = append(einoTools, taskCompleteTool{})

	// 3. Build the executor. Wrap the model so each executor step forces a
	//    tool call — Volces Ark and similar OpenAI-compatible providers do not
	//    reliably call tools under tool_choice=auto, which would otherwise
	//    surface as "[NodeRunError] no tool call" in the executor.
	return planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
		Model: newForceToolModel(execModel),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: einoTools,
			},
		},
		// Within one loop iteration the executor may iterate up to 999999
		// times to fully execute the current step. The outer plan-execute-replan
		// loop is bounded by MaxIterations in planexecute.Config.
		MaxIterations: 999999,
	})
}
