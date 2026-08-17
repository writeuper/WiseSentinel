package chat

import (
	"context"
	"errors"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type completionRepairRouter struct{ model model.ToolCallingChatModel }

func (r completionRepairRouter) ChatModel(context.Context, domain.ModelProfile) (any, error) {
	return r.model, nil
}

type completionRepairModel struct {
	tools  []*schema.ToolInfo
	choice *schema.ToolChoice
	result *schema.Message
}

func (m *completionRepairModel) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	options := model.GetCommonOptions(nil, opts...)
	m.choice = options.ToolChoice
	return m.result, nil
}

func (*completionRepairModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream is not used by completion repair")
}

func (m *completionRepairModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.tools = tools
	return m, nil
}

func TestRequestMissingTaskCompletionForcesCompletedProposal(t *testing.T) {
	fake := &completionRepairModel{result: &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{
		Name:      "task_complete",
		Arguments: `{"status":"completed","summary":"evidence collected","checklist":[{"item":"evidence","completed":true}]}`,
	}}}}}
	var proposal domain.TaskCompletion
	ctx := ctxkeys.WithTaskCompletionSink(context.Background(), &proposal)
	agent := NewAgent(completionRepairRouter{model: fake}, nil, nil)
	if err := agent.requestMissingTaskCompletion(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.choice == nil || *fake.choice != schema.ToolChoiceForced {
		t.Fatalf("tool choice = %v, want forced", fake.choice)
	}
	if len(fake.tools) != 1 || fake.tools[0].Name != "task_complete" {
		t.Fatalf("bound tools = %#v, want only task_complete", fake.tools)
	}
	if proposal.Status != "completed" || proposal.Summary == "" {
		t.Fatalf("proposal = %#v", proposal)
	}
}

func TestCanRepairMissingCompletionRequiresSuccessfulAllowedEvidence(t *testing.T) {
	contract := domain.TaskContract{AllowedTools: []string{"citation", "query_internal_docs"}}
	if !canRepairMissingCompletion(contract, []domain.Evidence{{ToolName: "citation", Status: "success"}}) {
		t.Fatal("citation evidence should permit repair")
	}
	if canRepairMissingCompletion(contract, []domain.Evidence{{ToolName: "query_internal_docs", Status: "error"}}) {
		t.Fatal("failed evidence must not permit repair")
	}
	if canRepairMissingCompletion(contract, []domain.Evidence{{ToolName: "unknown", Status: "success"}}) {
		t.Fatal("out-of-scope evidence must not permit repair")
	}
}
