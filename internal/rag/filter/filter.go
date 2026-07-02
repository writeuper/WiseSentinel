package filter

import (
	"fmt"
	"strings"
)

// RetrieveExpr builds a Milvus boolean expression for tenant-scoped retrieval.
func RetrieveExpr(tenantID string, docIDs []string, maxSecretLevel int) string {
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
	return strings.Join(parts, " && ")
}

// DeleteByDocIDExpr matches all chunks for a document.
func DeleteByDocIDExpr(docID string) string {
	return fmt.Sprintf(`metadata["doc_id"] == %s`, quote(docID))
}

// DeleteBySourceExpr matches chunks indexed from the same source URI.
func DeleteBySourceExpr(source string) string {
	return fmt.Sprintf(`metadata["_source"] == %s`, quote(source))
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
