package chat

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestTaskCompleteToolRecordsOnlyValidCompletionProposal(t *testing.T) {
	var proposal domain.TaskCompletion
	ctx := ctxkeys.WithTaskCompletionSink(context.Background(), &proposal)
	tool := taskCompleteTool{}
	if _, err := tool.InvokableRun(ctx, `{"status":"completed","summary":"logs checked","checklist":[{"item":"logs","completed":true}]}`); err != nil {
		t.Fatal(err)
	}
	if proposal.Summary != "logs checked" || len(proposal.Checklist) != 1 || !proposal.Checklist[0].Completed {
		t.Fatalf("proposal = %#v", proposal)
	}
	if _, err := tool.InvokableRun(ctx, `{"status":"in_progress","summary":"x","checklist":[]}`); err == nil {
		t.Fatal("non-terminal proposal was accepted")
	}
}
