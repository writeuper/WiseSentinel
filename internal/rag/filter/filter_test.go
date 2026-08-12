package filter_test

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/rag/filter"
)

func TestRetrieveExpr_TenantOnly(t *testing.T) {
	expr := filter.RetrieveExpr("default", nil, domain.SecretLevelInternal, nil)
	if !strings.Contains(expr, `metadata["tenant_id"] == "default"`) {
		t.Fatalf("unexpected expr: %s", expr)
	}
	if !strings.Contains(expr, `metadata["secret_level"] <= 1`) {
		t.Fatalf("expected secret_level filter, got: %s", expr)
	}
}

func TestRetrieveExpr_WithDocIDs(t *testing.T) {
	expr := filter.RetrieveExpr("default", []string{"doc-1", "doc-2"}, domain.SecretLevelSensitive, nil)
	if !strings.Contains(expr, `metadata["doc_id"] in ["doc-1", "doc-2"]`) {
		t.Fatalf("unexpected expr: %s", expr)
	}
	if !strings.Contains(expr, `metadata["secret_level"] <= 2`) {
		t.Fatalf("expected secret_level filter, got: %s", expr)
	}
}

func TestDeleteByDocIDExpr(t *testing.T) {
	expr := filter.DeleteByDocIDExpr("tenant-a", "abc")
	want := `metadata["tenant_id"] == "tenant-a" && metadata["doc_id"] == "abc"`
	if expr != want {
		t.Fatalf("got %q want %q", expr, want)
	}
}

func TestDeleteBySourceExprScopesTenant(t *testing.T) {
	expr := filter.DeleteBySourceExpr("tenant-a", "shared.md")
	want := `metadata["tenant_id"] == "tenant-a" && metadata["_source"] == "shared.md"`
	if expr != want {
		t.Fatalf("got %q want %q", expr, want)
	}
}

func TestDeleteByGenerationExprScopesTenantDocumentAndGeneration(t *testing.T) {
	expr := filter.DeleteByGenerationExpr("tenant-a", "doc-a", 7)
	want := `metadata["tenant_id"] == "tenant-a" && metadata["doc_id"] == "doc-a" && metadata["generation"] == 7`
	if expr != want {
		t.Fatalf("got %q want %q", expr, want)
	}
}

func TestDeleteByIDsExprQuotesEveryPrimaryKey(t *testing.T) {
	if got, want := filter.DeleteByIDsExpr([]string{"chunk-a", `chunk"b`}), `id in ["chunk-a", "chunk\"b"]`; got != want {
		t.Fatalf("expression = %q, want %q", got, want)
	}
	if got := filter.DeleteByIDsExpr(nil); got != "id in []" {
		t.Fatalf("empty expression = %q", got)
	}
}
