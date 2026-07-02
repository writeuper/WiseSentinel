package filter_test

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/rag/filter"
)

func TestRetrieveExpr_TenantOnly(t *testing.T) {
	expr := filter.RetrieveExpr("default", nil, domain.SecretLevelInternal)
	if !strings.Contains(expr, `metadata["tenant_id"] == "default"`) {
		t.Fatalf("unexpected expr: %s", expr)
	}
	if !strings.Contains(expr, `metadata["secret_level"] <= 1`) {
		t.Fatalf("expected secret_level filter, got: %s", expr)
	}
}

func TestRetrieveExpr_WithDocIDs(t *testing.T) {
	expr := filter.RetrieveExpr("default", []string{"doc-1", "doc-2"}, domain.SecretLevelSensitive)
	if !strings.Contains(expr, `metadata["doc_id"] in ["doc-1", "doc-2"]`) {
		t.Fatalf("unexpected expr: %s", expr)
	}
	if !strings.Contains(expr, `metadata["secret_level"] <= 2`) {
		t.Fatalf("expected secret_level filter, got: %s", expr)
	}
}

func TestDeleteByDocIDExpr(t *testing.T) {
	expr := filter.DeleteByDocIDExpr("abc")
	want := `metadata["doc_id"] == "abc"`
	if expr != want {
		t.Fatalf("got %q want %q", expr, want)
	}
}
