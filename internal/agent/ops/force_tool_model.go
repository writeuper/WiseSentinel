package ops

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// forceToolModel wraps a ToolCallingChatModel and forces tool_choice to "forced"
// on every Generate/Stream call.
//
// Why: Eino's planexecute Executor expects the model to emit a tool call for each
// step. OpenAI-compatible providers like Volces Ark do not reliably call tools
// when tool_choice is unset (defaults to "auto"), causing "[NodeRunError] no tool
// call". Forcing tool_choice (normalized to "required" in the model adapter)
// makes the executor robust against providers that otherwise prefer text answers.
//
// This wrapper is only applied to the Ops Agent executor, so the Chat Agent is
// unaffected and still allows the model to answer directly when no tool is needed.
type forceToolModel struct {
	inner model.ToolCallingChatModel
}

// newForceToolModel wraps m so every call forces a tool choice.
func newForceToolModel(m model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &forceToolModel{inner: m}
}

// Generate delegates to the inner model with an injected tool_choice option.
func (w *forceToolModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return w.inner.Generate(ctx, input, append(forceToolChoiceOpts(opts), model.WithToolChoice(schema.ToolChoice("forced")))...)
}

// Stream delegates to the inner model with an injected tool_choice option.
func (w *forceToolModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return w.inner.Stream(ctx, input, append(forceToolChoiceOpts(opts), model.WithToolChoice(schema.ToolChoice("forced")))...)
}

// WithTools binds tools on the inner model and re-wraps the result so the
// forced tool choice is preserved across the executor's tool binding.
func (w *forceToolModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := w.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &forceToolModel{inner: inner}, nil
}

// forceToolChoiceOpts returns the original opts verbatim; append is used at the
// call site to guarantee WithToolChoice is always the last option so it wins
// when the option applier processes them in order.
func forceToolChoiceOpts(opts []model.Option) []model.Option {
	out := make([]model.Option, 0, len(opts)+1)
	out = append(out, opts...)
	return out
}

// Ensure adk.LLMModel compatibility is preserved through embedding where needed.
var _ model.ToolCallingChatModel = (*forceToolModel)(nil)
