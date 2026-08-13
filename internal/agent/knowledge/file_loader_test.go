package knowledge

import (
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestDocumentVersionUsesBusinessVersionOrDurableGeneration(t *testing.T) {
	if got := documentVersion(&domain.IndexTaskRequest{Version: "runbook-v2", Generation: 4}); got != "runbook-v2" {
		t.Fatalf("business version = %q", got)
	}
	if got := documentVersion(&domain.IndexTaskRequest{Generation: 4}); got != "generation-4" {
		t.Fatalf("generation version = %q", got)
	}
	if got := documentVersion(&domain.IndexTaskRequest{}); got != "" {
		t.Fatalf("legacy version = %q, want empty", got)
	}
}
