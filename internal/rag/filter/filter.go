package filter

import (
	"fmt"
	"strings"
)

// RetrieveExpr builds a Milvus boolean expression for tenant-scoped retrieval.
func RetrieveExpr(tenantID string, docIDs []string, maxSecretLevel int, excludeSources []string) string {
	parts := []string{
		fmt.Sprintf(`metadata["tenant_id"] == %s`, quote(tenantID)),
		`metadata["visibility"] in ["tenant", "team"]`,
	}
	if maxSecretLevel > 0 {
		parts = append(parts, fmt.Sprintf(`metadata["secret_level"] <= %d`, maxSecretLevel))
	}
	if len(docIDs) > 0 {
		parts = append(parts, fmt.Sprintf(`metadata["doc_id"] in [%s]`, joinQuoted(docIDs)))
	}
	for _, source := range excludeSources {
		parts = append(parts, fmt.Sprintf(`metadata["_source"] != %s`, quote(source)))
	}
	return strings.Join(parts, " && ")
}

// DeleteByDocIDExpr matches chunks for one document within a tenant.
func DeleteByDocIDExpr(tenantID, docID string) string {
	return fmt.Sprintf(`metadata["tenant_id"] == %s && metadata["doc_id"] == %s`, quote(tenantID), quote(docID))
}

// DeleteBySourceExpr matches chunks indexed from the same source URI within a tenant.
func DeleteBySourceExpr(tenantID, source string) string {
	return fmt.Sprintf(`metadata["tenant_id"] == %s && metadata["_source"] == %s`, quote(tenantID), quote(source))
}

// DeleteByGenerationExpr matches one staged generation within a single tenant
// and document. GC must never use a generation-only or doc-only expression.
func DeleteByGenerationExpr(tenantID, docID string, generation uint64) string {
	return fmt.Sprintf(`metadata["tenant_id"] == %s && metadata["doc_id"] == %s && metadata["generation"] == %d`, quote(tenantID), quote(docID), generation)
}

// DeleteByIDsExpr is used only after a tenant-and-document-scoped scan has
// identified legacy primary keys. It intentionally never accepts an empty set.
func DeleteByIDsExpr(ids []string) string {
	if len(ids) == 0 {
		return "id in []"
	}
	return fmt.Sprintf("id in [%s]", joinQuoted(ids))
}

func quote(s string) string {
	return fmt.Sprintf("%q", s)
}

func joinQuoted(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = quote(item)
	}
	return strings.Join(quoted, ", ")
}
