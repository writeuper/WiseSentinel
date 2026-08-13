package handler

import (
	"context"
	"testing"
)

func TestKnowledgeHandlersRejectNilRequests(t *testing.T) {
	controller := NewV1(nil)
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"upload", func() error { _, err := controller.UploadDocument(ctx, nil); return err }},
		{"list_documents", func() error { _, err := controller.ListDocuments(ctx, nil); return err }},
		{"delete_document", func() error { _, err := controller.DeleteDocument(ctx, nil); return err }},
		{"reindex_document", func() error { _, err := controller.ReindexDocument(ctx, nil); return err }},
		{"get_index_task", func() error { _, err := controller.GetIndexTask(ctx, nil); return err }},
		{"list_fault_knowledge", func() error { _, err := controller.ListFaultKnowledge(ctx, nil); return err }},
		{"approve_fault_knowledge", func() error { _, err := controller.ApproveFaultKnowledge(ctx, nil); return err }},
		{"reject_fault_knowledge", func() error { _, err := controller.RejectFaultKnowledge(ctx, nil); return err }},
		{"feedback_fault_knowledge", func() error { _, err := controller.FeedbackFaultKnowledge(ctx, nil); return err }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("nil request must return an error")
			}
		})
	}
}
