package handler

import (
	"testing"

	"wisesentinel-platform/internal/repository"
)

// The generic approval repository only records a decision; it is not an
// executor. Keep this source-level contract pinned so a later approval type
// cannot silently regain the dangerous generic approve fall-through.
func TestApprovalDecisionUnknownTypeRequiresDedicatedExecution(t *testing.T) {
	if !approvalRequiresDedicatedExecution("tool_invoke") {
		t.Fatal("generic tool approval must not fall through to a status-only approval")
	}
	if approvalRequiresDedicatedExecution(repository.VectorGCRedriveApprovalType) {
		t.Fatal("vector GC redrive has a dedicated atomic approval-and-requeue path")
	}
}
