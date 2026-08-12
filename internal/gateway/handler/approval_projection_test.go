package handler

import (
	"testing"

	"wisesentinel-platform/internal/repository"
)

func TestApprovalTargetProjectionShowsOnlyVectorGCTarget(t *testing.T) {
	approval := &repository.Approval{
		ApprovalType: repository.VectorGCRedriveApprovalType,
		PayloadJSON:  `{"doc_id":"doc-a","target_key":"generation:7","requested_by":"admin-a","reason":"token=secret"}`,
	}
	got := approvalTargetProjection(approval)
	if got == nil || got.Kind != "vector_gc_redrive" || got.DocID != "doc-a" || got.TargetKey != "generation:7" {
		t.Fatalf("projection = %#v", got)
	}
}

func TestApprovalTargetProjectionRejectsNonVectorOrMalformedPayload(t *testing.T) {
	if got := approvalTargetProjection(&repository.Approval{ApprovalType: "tool_invoke", PayloadJSON: `{"doc_id":"doc-a"}`}); got != nil {
		t.Fatalf("non-vector projection = %#v", got)
	}
	if got := approvalTargetProjection(&repository.Approval{ApprovalType: repository.VectorGCRedriveApprovalType, PayloadJSON: `{"doc_id":"doc-a","reason":"token=secret"}`}); got != nil {
		t.Fatalf("incomplete projection = %#v", got)
	}
}
