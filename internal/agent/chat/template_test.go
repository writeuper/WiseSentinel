package chat

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestChatTemplateUsesConfiguredPromptButRetainsSafetyRules(t *testing.T) {
	prompt := NewChatTemplate("doc context", "你是订单平台专属助手")
	got := prompt.BuildSystemPromptStatic("fixed-now")
	for _, want := range []string{"你是订单平台专属助手", "工具调用失败时说明工具失败原因", "doc context", "fixed-now"} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt missing %q: %s", want, got)
		}
	}
}

func TestRuntimeMaxStepsNeverExceedsBuiltInSafetyBound(t *testing.T) {
	if got := runtimeMaxSteps(&domain.AgentRuntimeConfig{MaxIterations: 30}, maxStep); got != maxStep {
		t.Fatalf("configured max steps exceeded built-in bound: %d", got)
	}
	if got := runtimeMaxSteps(&domain.AgentRuntimeConfig{MaxIterations: 5}, maxStep); got != 5 {
		t.Fatalf("configured max steps ignored: %d", got)
	}
}
