package knowledge

import (
	"context"
	"fmt"
	"os"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/splitter"
)

// Pipeline runs FileLoader → MarkdownSplitter → MilvusIndexer.
type Pipeline struct {
	store   *storage.LocalStore
	indexer *indexer.MilvusIndexer
}

func NewPipeline(store *storage.LocalStore, idx *indexer.MilvusIndexer) *Pipeline {
	return &Pipeline{store: store, indexer: idx}
}

// IndexDocument loads, splits, deletes old chunks, and indexes a document.
func (p *Pipeline) IndexDocument(ctx context.Context, req *domain.IndexTaskRequest) (int, error) {
	if req == nil {
		return 0, fmt.Errorf("index request is nil")
	}
	if req.SourceURI == "" {
		return 0, fmt.Errorf("source uri is required")
	}

	raw, err := p.store.Read(req.SourceURI)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("document file not found: %s", req.SourceURI)
		}
		return 0, err
	}

	chunks := splitter.SplitMarkdown(string(raw))
	if len(chunks) == 0 {
		return 0, fmt.Errorf("no chunks produced from document")
	}

	if err := p.indexer.DeleteBySource(ctx, req.SourceURI); err != nil {
		return 0, fmt.Errorf("delete old chunks: %w", err)
	}
	if req.DocID != "" {
		if err := p.indexer.DeleteByDocID(ctx, req.DocID); err != nil {
			return 0, fmt.Errorf("delete old doc chunks: %w", err)
		}
	}

	inputs := indexer.BuildChunkInputs(chunks, req.TenantID, req.DocID, req.SourceURI, req.Visibility, req.SecretLevel)
	return p.indexer.IndexChunks(ctx, inputs)
}
