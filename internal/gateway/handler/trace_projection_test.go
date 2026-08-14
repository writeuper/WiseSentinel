package handler

import (
	"testing"

	"wisesentinel-platform/internal/repository"
)

func TestTraceProjectionPreservesStructuredEvidenceButNotBodies(t *testing.T) {
	steps := toAgentTraceSteps([]repository.AgentTraceStep{{
		StepType:       "rag",
		StepName:       "retrieve",
		OutputSummary:  `{"suppressed":true,"bytes":123}`,
		EvidenceDocIDs: []string{"doc-a", "doc-b"},
	}})
	if len(steps) != 1 || len(steps[0].EvidenceDocIDs) != 2 || steps[0].EvidenceDocIDs[0] != "doc-a" {
		t.Fatalf("trace evidence projection = %#v", steps)
	}
	if steps[0].OutputSummary != `{"suppressed":true,"bytes":123}` {
		t.Fatalf("unexpected output summary projection: %q", steps[0].OutputSummary)
	}
}
