package knowledge

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag/indexer"

	"github.com/cloudwego/eino/components/document"
)

// Pipeline orchestrates incremental delete and the Eino index graph.
type Pipeline struct {
	graph   *IndexGraph
	indexer *indexer.MilvusIndexer
}

// NewPipeline builds the knowledge indexing pipeline with an Eino graph.
func NewPipeline(ctx context.Context, store *storage.LocalStore, idx *indexer.MilvusIndexer) (*Pipeline, error) {
	graph, err := NewIndexGraph(ctx, store, idx)
	if err != nil {
		return nil, err
	}
	return &Pipeline{graph: graph, indexer: idx}, nil
}

// IndexDocument deletes stale chunks then invokes the index graph.
func (p *Pipeline) IndexDocument(ctx context.Context, req *domain.IndexTaskRequest) (int, error) {
	if req == nil {
		return 0, fmt.Errorf("index request is nil")
	}
	if req.SourceURI == "" {
		return 0, fmt.Errorf("source uri is required")
	}

	if err := p.indexer.DeleteBySource(ctx, req.SourceURI); err != nil {
		return 0, fmt.Errorf("delete old chunks: %w", err)
	}
	if req.DocID != "" {
		if err := p.indexer.DeleteByDocID(ctx, req.DocID); err != nil {
			return 0, fmt.Errorf("delete old doc chunks: %w", err)
		}
	}

	ctx = WithIndexTask(ctx, req)
	return p.graph.Invoke(ctx, document.Source{URI: req.SourceURI})
}
