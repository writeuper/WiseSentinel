package knowledge

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag/indexer"

	"github.com/cloudwego/eino/components/document"
)

// Pipeline stages a document generation through the Eino index graph.
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

// IndexDocument writes only the requested staged generation. Publication and
// cleanup are separate operations so a failed/reclaimed worker cannot remove a
// currently active generation.
func (p *Pipeline) IndexDocument(ctx context.Context, req *domain.IndexTaskRequest) (int, error) {
	if req == nil {
		return 0, fmt.Errorf("index request is nil")
	}
	if req.SourceURI == "" {
		return 0, fmt.Errorf("source uri is required")
	}
	if req.Generation == 0 || req.TaskID == "" {
		return 0, fmt.Errorf("index generation and task ID are required")
	}

	ctx = WithIndexTask(ctx, req)
	// 调用Index Graph，创建索引
	return p.graph.Invoke(ctx, document.Source{URI: req.SourceURI})
}
