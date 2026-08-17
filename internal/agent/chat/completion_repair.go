package chat

import (
	"context"
	"fmt"
	"strings"

	"wisesentinel-platform/internal/domain"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// requestMissingTaskCompletion gives the model one narrow, forced tool-call
// turn to submit the completion proposal it omitted from the main ReAct loop.
// It never manufactures a completion locally: the proposal must still be a
// model-emitted task_complete call and is subsequently validated against the
// platform-recorded evidence.
func (a *Agent) requestMissingTaskCompletion(ctx context.Context) error {
	if a.modelRouter == nil {
		return fmt.Errorf("model router is unavailable for task completion repair")
	}
	rawModel, err := a.modelRouter.ChatModel(ctx, domain.ModelProfileChatFast)
	if err != nil {
		return fmt.Errorf("get completion repair model: %w", err)
	}
	chatModel, ok := rawModel.(model.ToolCallingChatModel)
	if !ok {
		return fmt.Errorf("completion repair model does not implement ToolCallingChatModel")
	}
	info, err := (taskCompleteTool{}).Info(ctx)
	if err != nil {
		return fmt.Errorf("describe task_complete: %w", err)
	}
	bound, err := chatModel.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		return fmt.Errorf("bind task_complete: %w", err)
	}
	response, err := bound.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You are completing an already evidence-backed task. Call task_complete exactly once now. The call must use status=completed, a non-empty evidence-based summary, and a checklist whose items are all completed=true. Do not provide prose instead of the tool call."),
		schema.UserMessage("Submit the required task_complete proposal."),
	}, model.WithToolChoice(schema.ToolChoiceForced))
	if err != nil {
		return fmt.Errorf("generate task_complete proposal: %w", err)
	}
	if response == nil {
		return fmt.Errorf("completion repair model returned no response")
	}
	var args string
	for _, call := range response.ToolCalls {
		if call.Function.Name != "task_complete" {
			return fmt.Errorf("completion repair returned unexpected tool %q", call.Function.Name)
		}
		if args != "" {
			return fmt.Errorf("completion repair returned multiple task_complete calls")
		}
		args = call.Function.Arguments
	}
	if strings.TrimSpace(args) == "" {
		return fmt.Errorf("completion repair did not return task_complete")
	}
	if _, err := (taskCompleteTool{}).InvokableRun(ctx, args); err != nil {
		return fmt.Errorf("validate completion repair proposal: %w", err)
	}
	return nil
}

// canRepairMissingCompletion prevents the repair call from being used to
// conceal missing or failed evidence. It is intentionally stricter than a
// plain "any evidence" test: error/timeout evidence blocks a completed claim.
func canRepairMissingCompletion(contract domain.TaskContract, evidence []domain.Evidence) bool {
	allowed := make(map[string]struct{}, len(contract.AllowedTools))
	for _, name := range contract.AllowedTools {
		if name = strings.TrimSpace(name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	for _, item := range evidence {
		if item.Status == "error" || item.Status == "timeout" {
			return false
		}
		if item.Status != "success" {
			continue
		}
		if _, ok := allowed[item.ToolName]; ok {
			return true
		}
	}
	return false
}
