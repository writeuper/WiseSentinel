package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"wisesentinel-platform/internal/domain"
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

	resp, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		Query: req.Query,
		TopK:  3,
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