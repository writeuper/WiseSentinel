//go:build integration

package rag_test

import (
	"os"
	"path/filepath"
	"testing"

	"wisesentinel-platform/internal/agent/knowledge"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/storage"
	"wisesentinel-platform/internal/rag"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/rag/splitter"
	"wisesentinel-platform/internal/repository"

	"github.com/google/uuid"
	"github.com/gogf/gf/v2/os/gctx"
)

func setupRAG(t *testing.T) (*rag.Service, *knowledge.Pipeline, *indexer.MilvusIndexer, func()) {
	t.Helper()

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
	cleanup := func() { milvusClient.Close() }

	emb := embedder.NewHashEmbedder()
	idx := indexer.NewMilvusIndexer(milvusClient, emb)
	retr := retriever.NewMilvusRetriever(milvusClient, emb)
	store := storage.NewLocalStore(ctx)
	pipeline, err := knowledge.NewPipeline(ctx, store, idx)
	if err != nil {
		t.Fatalf("pipeline init: %v", err)
	}

	svc := rag.NewService(pipeline, retr, idx, repository.NewIndexTaskRepo())
	return svc, pipeline, idx, cleanup
}

func readFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "knowledge", "alert_runbook.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

func TestRAGIndexRetrieveLoop(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	raw := readFixture(t)
	tenantID := "it-" + uuid.NewString()[:8]
	docID := uuid.NewString()
	source := filepath.Join(t.TempDir(), "alert_runbook.md")

	chunks := splitter.SplitMarkdown(raw)
	if len(chunks) == 0 {
		t.Fatal("no chunks from fixture")
	}

	inputs := indexer.BuildChunkInputs(chunks, tenantID, docID, source, "tenant", domain.SecretLevelInternal)
	_ = idx.DeleteByDocID(ctx, docID)

	count, err := idx.IndexChunks(ctx, inputs)
	if err != nil {
		t.Fatalf("index chunks: %v", err)
	}
	if count == 0 {
		t.Fatal("expected chunk count > 0")
	}

	resp, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID:       tenantID,
		Query:          chunks[0].Content,
		TopK:           3,
		MaxSecretLevel: domain.SecretLevelInternal,
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

func TestPipelineIncrementalReindex(t *testing.T) {
	svc, pipeline, _, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantID := "it-" + uuid.NewString()[:8]
	docID := uuid.NewString()
	dir := t.TempDir()
	source := filepath.Join(dir, "runbook.md")
	if err := os.WriteFile(source, []byte("# Runbook\n\nFirst version.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := &domain.IndexTaskRequest{
		TenantID:    tenantID,
		DocID:       docID,
		SourceURI:   source,
		Visibility:  "tenant",
		SecretLevel: domain.SecretLevelInternal,
	}

	count1, err := pipeline.IndexDocument(ctx, req)
	if err != nil {
		t.Fatalf("first index: %v", err)
	}
	if count1 == 0 {
		t.Fatal("expected chunks after first index")
	}

	if err := os.WriteFile(source, []byte("# Runbook\n\nSecond version with more detail.\n\n## Steps\n\nDo something.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count2, err := pipeline.IndexDocument(ctx, req)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if count2 <= count1 {
		t.Fatalf("reindex chunk count = %d, want > %d", count2, count1)
	}

	resp, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID:       tenantID,
		Query:          "Second version",
		TopK:           3,
		MaxSecretLevel: domain.SecretLevelInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Documents) == 0 {
		t.Fatal("expected hits after reindex")
	}
	found := false
	for _, doc := range resp.Documents {
		if doc.DocID == docID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected reindexed doc_id in results")
	}
}

func TestTenantIsolation(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantA := "it-a-" + uuid.NewString()[:6]
	tenantB := "it-b-" + uuid.NewString()[:6]
	docA := uuid.NewString()
	docB := uuid.NewString()
	sourceA := filepath.Join(t.TempDir(), "a.md")
	sourceB := filepath.Join(t.TempDir(), "b.md")

	content := "# Shared Keyword XYZ\n\nTenant specific content."
	if err := os.WriteFile(sourceA, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceB, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		tenantID, docID, source string
	}{
		{tenantA, docA, sourceA},
		{tenantB, docB, sourceB},
	} {
		chunks := splitter.SplitMarkdown(content)
		inputs := indexer.BuildChunkInputs(chunks, tc.tenantID, tc.docID, tc.source, "tenant", domain.SecretLevelInternal)
		_ = idx.DeleteByDocID(ctx, tc.docID)
		if _, err := idx.IndexChunks(ctx, inputs); err != nil {
			t.Fatalf("index %s: %v", tc.tenantID, err)
		}
	}

	resp, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID:       tenantA,
		Query:          "Shared Keyword XYZ",
		TopK:           5,
		MaxSecretLevel: domain.SecretLevelInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range resp.Documents {
		if doc.DocID == docB {
			t.Fatalf("tenant A retrieved tenant B document: %s", doc.DocID)
		}
	}
}

func TestSecretLevelFilter(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantID := "it-" + uuid.NewString()[:8]
	docID := uuid.NewString()
	source := filepath.Join(t.TempDir(), "secret.md")
	content := "# Sensitive Runbook\n\nConfidential escalation steps."

	chunks := splitter.SplitMarkdown(content)
	inputs := indexer.BuildChunkInputs(chunks, tenantID, docID, source, "tenant", domain.SecretLevelSensitive)
	_ = idx.DeleteByDocID(ctx, docID)
	if _, err := idx.IndexChunks(ctx, inputs); err != nil {
		t.Fatal(err)
	}

	allowed, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID:       tenantID,
		Query:          "Confidential escalation",
		TopK:           3,
		MaxSecretLevel: domain.SecretLevelSensitive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allowed.Documents) == 0 {
		t.Fatal("admin-level retrieve should hit sensitive doc")
	}

	blocked, err := svc.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID:       tenantID,
		Query:          "Confidential escalation",
		TopK:           3,
		MaxSecretLevel: domain.SecretLevelInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Documents) != 0 {
		t.Fatalf("internal-level retrieve should not see sensitive doc, got %d hits", len(blocked.Documents))
	}
}
