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

func TestValidateAgentConfigJSONRejectsUnsafeActivation(t *testing.T) {
	tests := []string{
		`{"max_iterations":101}`,
		`{"tools":["query_logs","query_logs"]}`,
		`{"system_prompt":123}`,
		`{"chunk_size":50}`,
	}
	for _, raw := range tests {
		typeName := "chat"
		if raw == `{"chunk_size":50}` {
			typeName = string(domain.AgentTypeKnowledge)
		}
		if err := ValidateAgentConfigJSON(typeName, "v-invalid", raw); err == nil {
			t.Fatalf("unsafe config accepted: %s", raw)
		}
	}
}

func TestValidateAgentConfigJSONAcceptsBoundedConfig(t *testing.T) {
	raw := `{"system_prompt":"safe","max_iterations":20,"tools":["query_logs"],"chunk_size":500,"overlap":50}`
	if err := ValidateAgentConfigJSON(string(domain.AgentTypeKnowledge), "v1", raw); err != nil {
		t.Fatalf("bounded config rejected: %v", err)
	}
}
