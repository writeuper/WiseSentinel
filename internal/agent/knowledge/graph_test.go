package knowledge_test

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/agent/knowledge"
	"wisesentinel-platform/internal/rag/indexer"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/os/gctx"
)

func TestNewIndexGraph_Compile(t *testing.T) {
	ctx := gctx.New()
	graph, err := knowledge.NewIndexGraph(ctx, nil, &indexer.MilvusIndexer{})
	if err != nil {
		t.Fatalf("compile graph: %v", err)
	}
	if graph == nil {
		t.Fatal("expected graph instance")
	}
}

func TestMarkdownTransformer_Split(t *testing.T) {
	transformer := knowledge.NewMarkdownTransformer()
	docs, err := transformer.Transform(context.Background(), []*schema.Document{{
		Content: "# Title\n\nIntro\n\n## Section\n\nBody",
		MetaData: map[string]any{
			"tenant_id": "default",
			"doc_id":    "doc-1",
		},
	}})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(docs))
	}
}
