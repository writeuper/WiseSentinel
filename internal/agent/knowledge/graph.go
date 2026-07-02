package knowledge

import (
	"context"
	"fmt"

	ragindexer "wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/pkg/storage"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/compose"
)

const (
	nodeFileLoader         = "FileLoader"
	nodeMarkdownSplitter   = "MarkdownSplitter"
	nodeMilvusIndexer      = "MilvusIndexer"
)

// IndexGraph runs FileLoader → MarkdownSplitter → MilvusIndexer (Eino compose graph).
type IndexGraph struct {
	runner compose.Runnable[document.Source, int]
}

// NewIndexGraph builds and compiles the knowledge indexing graph.
func NewIndexGraph(ctx context.Context, store *storage.LocalStore, idx *ragindexer.MilvusIndexer) (*IndexGraph, error) {
	chain := compose.NewChain[document.Source, int]()
	chain.AppendLoader(NewFileLoader(store), compose.WithNodeName(nodeFileLoader))
	chain.AppendDocumentTransformer(NewMarkdownTransformer(), compose.WithNodeName(nodeMarkdownSplitter))
	chain.AppendIndexer(NewEinoMilvusIndexer(idx), compose.WithNodeName(nodeMilvusIndexer))
	chain.AppendLambda(compose.InvokableLambda(func(_ context.Context, ids []string) (int, error) {
		return len(ids), nil
	}))

	runner, err := chain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile knowledge index graph: %w", err)
	}
	return &IndexGraph{runner: runner}, nil
}

// Invoke executes the graph for a single document source.
func (g *IndexGraph) Invoke(ctx context.Context, src document.Source) (int, error) {
	if g == nil || g.runner == nil {
		return 0, fmt.Errorf("index graph is not initialized")
	}
	return g.runner.Invoke(ctx, src)
}
