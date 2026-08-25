package chat

import (
	"context"
	"encoding/json"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

// validateShortcutCompletion closes a platform-owned fast path with the same
// explicit completion contract used by the ReAct path. Fast paths do not have
// an LLM to emit task_complete, so the platform submits a narrowly constructed
// proposal only after the real tool evidence has been recorded.
func (a *Agent) validateShortcutCompletion(ctx context.Context, contract domain.TaskContract, summary string, evidence []domain.Evidence) domain.CompletionDecision {
	proposal := domain.TaskCompletion{
		Status:  "completed",
		Summary: summary,
		Checklist: []domain.ChecklistItem{
			{Item: "verified tool evidence", Completed: true},
			{Item: "summary", Completed: summary != ""},
		},
	}
	args, err := json.Marshal(proposal)
	if err != nil {
		return domain.CompletionDecision{Reason: domain.CompletionTaskIncomplete, Missing: []string{"task_complete"}}
	}
	if _, err = (taskCompleteTool{}).InvokableRun(ctx, string(args)); err != nil {
		return domain.CompletionDecision{Reason: domain.CompletionTaskIncomplete, Missing: []string{"task_complete"}}
	}
	// A shortcut has exactly one platform-executed tool action. Its task budget
	// is an upper bound, not the observed iteration count.
	decision := domain.ValidateTaskCompletion(contract, proposal, evidence, 1)
	observability.ObserveTaskCompletionCheck(decision.Reason)
	if sink := ctxkeys.StepSinkFrom(ctx); sink != nil {
		status := "rejected"
		if decision.Accepted {
			status = "success"
		}
		sink("completion", "task_complete", "", decision.Reason, status, 0, "")
	}
	return decision
}
