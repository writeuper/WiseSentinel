package rag

import (
	"strings"

	"wisesentinel-platform/internal/domain"
)

const maxRetrievalQueries = 4

// buildRetrievalQueries returns a bounded, deterministic query plan. LLM-based
// expansion can populate QueryVariants later; the platform keeps the original
// query first and never allows variants to change tenant or metadata filters.
func buildRetrievalQueries(req *domain.RetrieveRequest) ([]string, bool) {
	if req == nil {
		return nil, false
	}
	seen := make(map[string]struct{}, maxRetrievalQueries)
	queries := make([]string, 0, maxRetrievalQueries)
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" || len(queries) >= maxRetrievalQueries {
			return
		}
		key := strings.ToLower(q)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		queries = append(queries, q)
	}
	add(req.Query)
	for _, q := range req.QueryVariants {
		add(q)
	}
	if req.EnableQueryExpansion {
		for _, q := range deterministicVariants(req.Query) {
			add(q)
		}
	}
	return queries, len(queries) > 1
}

func deterministicVariants(query string) []string {
	q := strings.TrimSpace(strings.TrimRight(query, "？?!！。."))
	if q == "" || len([]rune(q)) > 80 {
		return nil
	}
	switch {
	case strings.Contains(q, "怎么处理") || strings.Contains(q, "如何处理"):
		return []string{strings.ReplaceAll(strings.ReplaceAll(q, "怎么处理", ""), "如何处理", "") + "排查步骤 处置流程"}
	case strings.Contains(q, "为什么") || strings.Contains(q, "原因"):
		return []string{q + " 根因 排查"}
	case strings.Contains(q, "什么是") || strings.Contains(q, "如何理解"):
		return []string{q + " 定义 说明"}
	default:
		return nil
	}
}
