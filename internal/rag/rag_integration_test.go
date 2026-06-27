//go:build integration

package rag_test

import (
	"os"
	"path/filepath"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/rag/splitter"

	"github.com/google/uuid"
	"github.com/gogf/gf/v2/os/gctx"
)

func TestRAGIndexRetrieveLoop(t *testing.T) {
	cfgPath, err := filepath.Abs(filepath.Join("..", "..", "manifest", "config"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv("GF_GCFG_PATH", cfgPath)

	ctx := gctx.New()
	milvusClient, err := client.NewMilvusClient(ctx)
	if err != nil {
		t.Skipf("milvus not available: %v", err)
	}
	defer milvusClient.Close()

	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "knowledge", "alert_runbook.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	emb := embedder.NewHashEmbedder()
	idx := indexer.NewMilvusIndexer(milvusClient, emb)
	retr := retriever.NewMilvusRetriever(milvusClient, emb)

	tenantID := "it-" + uuid.NewString()[:8]
	docID := uuid.NewString()
	source := filepath.Join(t.TempDir(), "alert_runbook.md")

	chunks := splitter.SplitMarkdown(string(raw))
	if len(chunks) == 0 {
		t.Fatal("no chunks from fixture")
	}

	inputs := indexer.BuildChunkInputs(chunks, tenantID, docID, source, "tenant", 1)
	_ = idx.DeleteByDocID(ctx, docID)

	count, err := idx.IndexChunks(ctx, inputs)
	if err != nil {
		t.Fatalf("index chunks: %v", err)
	}
	if count == 0 {
		t.Fatal("expected chunk count > 0")
	}

	resp, err := retr.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID: tenantID,
		Query:    chunks[0].Content,
		TopK:     3,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(resp.Documents) == 0 {
		t.Fatal("expected retrieval hits")
	}
	if resp.Documents[0].DocID != docID {
		t.Fatalf("doc_id = %q, want %q", resp.Documents[0].DocID, docID)
	}
}
