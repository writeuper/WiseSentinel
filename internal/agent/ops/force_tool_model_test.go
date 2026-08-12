package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type captureToolModel struct {
	choice *schema.ToolChoice
}

func TestClarificationForInsufficientContextIsActionableWithoutTooling(t *testing.T) {
	answer := clarificationForQuery("系统有点慢帮我看看")
	for _, expected := range []string{"信息不足", "需要补充", "服务名称", "时间范围", "指标"} {
		if !strings.Contains(answer, expected) {
			t.Fatalf("clarification missing %q: %q", expected, answer)
		}
	}
	if got := clarificationForQuery("order-service 最近 15 分钟错误率升高"); got != "" {
		t.Fatalf("specific investigation must not be rejected: %q", got)
	}
	if got := clarificationForQuery("order-service 健康检查"); got == "" {
		t.Fatal("target without diagnostic signal must not trigger data reads")
	}
}

func (m *captureToolModel) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	options := model.GetCommonOptions(nil, opts...)
	m.choice = options.ToolChoice
	return schema.AssistantMessage("ok", nil), nil
}

func (m *captureToolModel) Stream(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	options := model.GetCommonOptions(nil, opts...)
	m.choice = options.ToolChoice
	return nil, nil
}

func (m *captureToolModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func TestForceToolModelUsesForcedChoice(t *testing.T) {
	inner := &captureToolModel{}
	wrapped := newForceToolModel(inner)

	if _, err := wrapped.Generate(context.Background(), nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if inner.choice == nil || string(*inner.choice) != "forced" {
		t.Fatalf("Generate() tool choice = %v, want forced", inner.choice)
	}

	if _, err := wrapped.Stream(context.Background(), nil); err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if inner.choice == nil || string(*inner.choice) != "forced" {
		t.Fatalf("Stream() tool choice = %v, want forced", inner.choice)
	}
}

func TestForceToolModelChoiceOverridesCallerOption(t *testing.T) {
	inner := &captureToolModel{}
	wrapped := newForceToolModel(inner)

	if _, err := wrapped.Generate(context.Background(), nil, model.WithToolChoice(schema.ToolChoice("auto"))); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if inner.choice == nil || string(*inner.choice) != "forced" {
		t.Fatalf("Generate() tool choice = %v, want forced", inner.choice)
	}
}
