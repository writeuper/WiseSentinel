package middleware

import (
	"context"
	"testing"
)

type auditContextKey string

func TestDetachedAuditContextPreservesValuesAfterParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), auditContextKey("trace"), "trace-1"))
	cancelParent()
	ctx, cancel := detachedAuditContext(parent)
	defer cancel()
	if got := ctx.Value(auditContextKey("trace")); got != "trace-1" {
		t.Fatalf("audit context value = %v, want trace-1", got)
	}
	if ctx.Err() != nil {
		t.Fatalf("detached audit context unexpectedly canceled: %v", ctx.Err())
	}
}
