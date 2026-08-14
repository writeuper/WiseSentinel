package handler

import "testing"

func TestAgentConfigSHA256IsStableAndDoesNotExposePayload(t *testing.T) {
	raw := `{"system_prompt":"safe","max_iterations":20}`
	first := agentConfigSHA256(raw)
	second := agentConfigSHA256(raw)
	if first != second {
		t.Fatalf("digest is not stable: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("digest length = %d, want 64", len(first))
	}
	if first == raw || first == "" {
		t.Fatalf("digest exposed raw config: %q", first)
	}
}
