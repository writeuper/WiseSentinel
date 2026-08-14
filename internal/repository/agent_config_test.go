package repository

import (
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestDecodeAgentConfigDeduplicatesToolsAndPreservesVersion(t *testing.T) {
	got, err := decodeAgentConfig(agentConfigRow{
		AgentType:  "chat",
		Version:    "v2",
		ConfigJSON: `{"system_prompt":"safe prompt","max_iterations":30,"tools":["query_logs","query_logs"," get_current_time ",""]}`,
	})
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if got.AgentType != domain.AgentTypeChat || got.Version != "v2" || got.SystemPrompt != "safe prompt" || got.MaxIterations != 30 {
		t.Fatalf("decoded config = %#v", got)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "query_logs" || got.Tools[1] != "get_current_time" {
		t.Fatalf("decoded tools = %#v", got.Tools)
	}
}

func TestDecodeAgentConfigRejectsUnsafeIterationBounds(t *testing.T) {
	for _, raw := range []string{`{"max_iterations":-1}`, `{"max_iterations":101}`} {
		if _, err := decodeAgentConfig(agentConfigRow{AgentType: "ops", Version: "bad", ConfigJSON: raw}); err == nil {
			t.Fatalf("config %s accepted", raw)
		}
	}
}
