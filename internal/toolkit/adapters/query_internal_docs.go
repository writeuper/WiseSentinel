package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

// QueryInternalDocsInput is the input for query_internal_docs.
type QueryInternalDocsInput struct {
	Query string `json:"query"`
}

// internalDocsRAG holds the RAG service reference for the query_internal_docs adapter.
var internalDocsRAG struct {
	mu  sync.RWMutex
	svc domain.RAGService
}

// SetRAGServiceForInternalDocs sets the RAG service used by query_internal_docs.
// Called during bootstrap wiring.
func SetRAGServiceForInternalDocs(svc domain.RAGService) {
	internalDocsRAG.mu.Lock()
	internalDocsRAG.svc = svc
	internalDocsRAG.mu.Unlock()
}

// QueryInternalDocs retrieves documents from the knowledge base.
func QueryInternalDocs(ctx context.Context, input json.RawMessage) (string, error) {
	internalDocsRAG.mu.RLock()
	svc := internalDocsRAG.svc
	internalDocsRAG.mu.RUnlock()

	if svc == nil {
		return "", fmt.Errorf("RAG service is not initialized")
	}

	var req QueryInternalDocsInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if req.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	resp, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID:             ctxkeys.TenantIDFrom(ctx),
		Query:                req.Query,
		EnableQueryExpansion: true,
		TopK:                 internalDocsTopK(req.Query),
	})
	if err != nil {
		return "", fmt.Errorf("retrieve failed: %w", err)
	}

	result, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(result), nil
}

func internalDocsTopK(query string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(q, "runbook") || strings.Contains(q, "排查手册") ||
		strings.Contains(q, "内部文档") || strings.Contains(q, "知识库") ||
		(strings.Contains(q, "检查顺序") && (strings.Contains(q, "止血") || strings.Contains(q, "升级"))) {
		return 8
	}
	return 3
}
