package middleware

import "testing"

func TestAuditActionClassifiesVectorGCRedriveSeparately(t *testing.T) {
	if got := auditAction("/api/v1/admin/vector-gc/tasks", httpMethodGet); got != "vector_gc.read" {
		t.Fatalf("list action = %q", got)
	}
	if got := auditAction("/api/v1/admin/vector-gc/documents/doc-a/tasks/generation:1/redrive", httpMethodPost); got != "vector_gc.redrive.request" {
		t.Fatalf("redrive action = %q", got)
	}
}
