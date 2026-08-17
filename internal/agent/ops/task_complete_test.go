package ops

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestOpsTaskCompleteToolCapturesOnlyExplicitCompletedProposal(t *testing.T) {
	var proposal domain.TaskCompletion
	ctx := ctxkeys.WithTaskCompletionSink(context.Background(), &proposal)
	if _, err := (taskCompleteTool{}).InvokableRun(ctx, `{"status":"completed","summary":"logs checked","checklist":[{"item":"logs","completed":true}]}`); err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "completed" || proposal.Summary != "logs checked" {
		t.Fatalf("proposal = %#v", proposal)
	}
	if _, err := (taskCompleteTool{}).InvokableRun(ctx, `{"status":"working","summary":"x","checklist":[]}`); err == nil {
		t.Fatal("non-completed status was accepted")
	}
}
