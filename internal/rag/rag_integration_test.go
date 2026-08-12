//go:build integration

package rag_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/google/uuid"
)

func setupRAG(t *testing.T) (*rag.Service, *knowledge.Pipeline, *indexer.MilvusIndexer, func()) {
	t.Helper()
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("OPS_TEST_MYSQL_DSN is required for RAG integration tests")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})

	cfgPath, err := filepath.Abs(filepath.Join("..", "..", "manifest", "config"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv("GF_GCFG_PATH", cfgPath)

	ctx := gctx.New()
	// Each test owns a short-lived collection.  Reusing the production-like
	// "biz" collection leaves delete segments behind and makes test latency
	// depend on unrelated runs.  This name is generated locally and the cleanup
	// below drops exactly this collection, never a shared collection.
	collection := "it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	t.Setenv("MILVUS_COLLECTION", collection)
	milvusClient, err := client.NewMilvusClient(ctx)
	if err != nil {
		t.Fatalf("connect Milvus for RAG integration test: %v", err)
	}
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := milvusClient.Client().DropCollection(cleanupCtx, collection); err != nil {
			t.Errorf("drop isolated Milvus collection %q: %v", collection, err)
		}
		milvusClient.Close()
	}

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
	_ = idx.DeleteByDocID(ctx, tenantID, docID)
	if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
		TenantID: tenantID, DocID: docID, Name: "alert_runbook.md", SourceURI: source,
		MimeType: "text/markdown", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "integration-test",
	}); err != nil {
		t.Fatalf("create document: %v", err)
	}
	t.Cleanup(func() {
		_ = idx.DeleteByDocID(ctx, tenantID, docID)
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})

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

// TestRAGEvaluationMetrics is a small, deterministic integration evaluation.
// It intentionally uses one labelled relevant document per query so Recall@K,
// MRR and nDCG have an unambiguous oracle.  It is a regression gate, not a
// claim about production corpus quality; production evaluation needs a larger
// reviewed dataset and a separately versioned relevance judgement set.
func TestRAGEvaluationMetrics(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantID := "it-eval-" + uuid.NewString()[:8]
	cases := []struct {
		query, content string
	}{
		{"payment gateway 503 runbook", "# Payment 503\n\nPayment gateway 503 runbook: inspect upstream status and retry budget."},
		{"order database deadlock runbook", "# Order deadlock\n\nOrder database deadlock runbook: inspect lock graph and retry transaction."},
		{"redis login timeout runbook", "# Redis login timeout\n\nRedis login timeout runbook: inspect Redis latency and connection pool."},
	}
	type labelledCase struct {
		query, docID string
	}
	labelled := make([]labelledCase, 0, len(cases))
	for number, tc := range cases {
		docID := uuid.NewString()
		source := filepath.Join(t.TempDir(), fmt.Sprintf("eval-%d.md", number))
		if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
			TenantID: tenantID, DocID: docID, Name: filepath.Base(source), SourceURI: source,
			MimeType: "text/markdown", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "integration-test",
		}); err != nil {
			t.Fatal(err)
		}
		inputs := indexer.BuildChunkInputs(splitter.SplitMarkdown(tc.content), tenantID, docID, source, "tenant", domain.SecretLevelInternal)
		if _, err := idx.IndexChunks(ctx, inputs); err != nil {
			t.Fatalf("index eval document %d: %v", number, err)
		}
		labelled = append(labelled, labelledCase{query: tc.query, docID: docID})
	}
	t.Cleanup(func() {
		for _, tc := range labelled {
			_ = idx.DeleteByDocID(ctx, tenantID, tc.docID)
		}
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})

	var hitAt1, hitAt3 int
	var reciprocalRank, ndcgAt3, precisionAt3 float64
	for _, tc := range labelled {
		response, err := svc.Retrieve(ctx, &domain.RetrieveRequest{TenantID: tenantID, Query: tc.query, TopK: 3, MaxSecretLevel: domain.SecretLevelInternal})
		if err != nil {
			t.Fatal(err)
		}
		rank := 0
		for index, doc := range response.Documents {
			if doc.DocID == tc.docID {
				rank = index + 1
				break
			}
		}
		if rank == 0 {
			t.Fatalf("labelled document was not retrieved for query %q", tc.query)
		}
		if rank == 1 {
			hitAt1++
		}
		if rank <= 3 {
			hitAt3++
			precisionAt3 += 1.0 / 3.0 // one relevant document is judged per query
			reciprocalRank += 1.0 / float64(rank)
			ndcgAt3 += 1.0 / (math.Log2(float64(rank) + 1))
		}
	}
	n := float64(len(labelled))
	t.Logf("RAG_EVAL corpus=%d top_k=3 recall_at_1=%.3f recall_at_3=%.3f precision_at_3=%.3f mrr=%.3f ndcg_at_3=%.3f", len(labelled), float64(hitAt1)/n, float64(hitAt3)/n, precisionAt3/n, reciprocalRank/n, ndcgAt3/n)
}

// TestConcurrentRAGRetrievalTenantIsolation exercises the production-shaped
// read path under concurrent user requests. The same Milvus client and MySQL
// generation resolver serve all requests, while an identically worded foreign
// tenant document ensures a missing tenant filter cannot accidentally pass.
func TestConcurrentRAGRetrievalTenantIsolation(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantA := "it-concurrent-a-" + uuid.NewString()[:8]
	tenantB := "it-concurrent-b-" + uuid.NewString()[:8]
	query := "order payment 503 retry runbook"
	docA, docB := uuid.NewString(), uuid.NewString()
	createAndIndex := func(tenantID, docID, content string) {
		t.Helper()
		source := filepath.Join(t.TempDir(), docID+".md")
		if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
			TenantID: tenantID, DocID: docID, Name: filepath.Base(source), SourceURI: source,
			MimeType: "text/markdown", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "integration-test",
		}); err != nil {
			t.Fatal(err)
		}
		inputs := indexer.BuildChunkInputs(splitter.SplitMarkdown(content), tenantID, docID, source, "tenant", domain.SecretLevelInternal)
		if _, err := idx.IndexChunks(ctx, inputs); err != nil {
			t.Fatal(err)
		}
	}
	createAndIndex(tenantA, docA, "# Tenant A\n\norder payment 503 retry runbook for the authorised tenant")
	createAndIndex(tenantB, docB, "# Tenant B\n\norder payment 503 retry runbook for a different tenant")
	t.Cleanup(func() {
		for _, entry := range []struct{ tenantID, docID string }{{tenantA, docA}, {tenantB, docB}} {
			_ = idx.DeleteByDocID(ctx, entry.tenantID, entry.docID)
			_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", entry.tenantID).Delete()
			_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", entry.tenantID).Delete()
		}
	})

	const parallelRequests = 24
	startedAt := time.Now()
	errs := make(chan error, parallelRequests)
	var completed atomic.Int32
	var wg sync.WaitGroup
	for request := 0; request < parallelRequests; request++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := svc.Retrieve(context.Background(), &domain.RetrieveRequest{
				TenantID: tenantA, Query: query, TopK: 3, MaxSecretLevel: domain.SecretLevelInternal,
			})
			if err != nil {
				errs <- err
				return
			}
			foundExpected := false
			for _, doc := range response.Documents {
				if doc.DocID == docB {
					errs <- fmt.Errorf("cross-tenant document %s returned", docB)
					return
				}
				if doc.DocID == docA {
					foundExpected = true
				}
			}
			if !foundExpected {
				errs <- fmt.Errorf("expected tenant document %s was not returned", docA)
				return
			}
			completed.Add(1)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := completed.Load(); got != parallelRequests {
		t.Fatalf("completed concurrent requests = %d, want %d", got, parallelRequests)
	}
	t.Logf("RAG_CONCURRENCY requests=%d tenant_leaks=0 elapsed_ms=%d", parallelRequests, time.Since(startedAt).Milliseconds())
}

func TestPipelineIncrementalReindex(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantID := "it-" + uuid.NewString()[:8]
	docID := uuid.NewString()
	dir := t.TempDir()
	source := filepath.Join(dir, "runbook.md")
	if err := os.WriteFile(source, []byte("# Runbook\n\nFirst version.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = idx.DeleteByDocID(ctx, tenantID, docID)
		_, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
		TenantID:    tenantID,
		DocID:       docID,
		Name:        "runbook.md",
		SourceURI:   source,
		MimeType:    "text/markdown",
		Visibility:  "tenant",
		SecretLevel: domain.SecretLevelInternal,
		Status:      "active",
		CreatedBy:   "integration-test",
	}); err != nil {
		t.Fatalf("create document: %v", err)
	}

	request := &domain.IndexTaskRequest{
		TenantID: tenantID, DocID: docID, SourceURI: source, Visibility: "tenant", SecretLevel: domain.SecretLevelInternal,
	}
	task1, err := svc.SubmitIndexTask(ctx, request)
	if err != nil {
		t.Fatalf("submit first index: %v", err)
	}
	if claimed, err := repository.NewIndexTaskRepo().ClaimRunnable(ctx, tenantID, task1, "token-first", time.Now().Add(time.Hour)); err != nil || !claimed {
		t.Fatalf("claim first = %v, %v", claimed, err)
	}
	if err := svc.ExecuteIndexTask(ctx, tenantID, task1, "token-first"); err != nil {
		t.Fatalf("execute first index: %v", err)
	}

	if err := os.WriteFile(source, []byte("# Runbook\n\nSecond version with more detail.\n\n## Steps\n\nDo something.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task2, err := svc.SubmitIndexTask(ctx, request)
	if err != nil {
		t.Fatalf("submit reindex: %v", err)
	}
	if claimed, err := repository.NewIndexTaskRepo().ClaimRunnable(ctx, tenantID, task2, "token-second", time.Now().Add(time.Hour)); err != nil || !claimed {
		t.Fatalf("claim second = %v, %v", claimed, err)
	}
	if err := svc.ExecuteIndexTask(ctx, tenantID, task2, "token-second"); err != nil {
		t.Fatalf("execute second index: %v", err)
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
	for _, doc := range resp.Documents {
		if doc.DocID != docID {
			continue
		}
		if generation, ok := doc.Metadata["generation"].(float64); !ok || generation != 2 {
			t.Fatalf("retrieved generation = %#v, want 2", doc.Metadata["generation"])
		}
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
		if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
			TenantID: tc.tenantID, DocID: tc.docID, Name: "tenant.md", SourceURI: tc.source,
			MimeType: "text/markdown", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "integration-test",
		}); err != nil {
			t.Fatalf("create document for %s: %v", tc.tenantID, err)
		}
		t.Cleanup(func() {
			_ = idx.DeleteByDocID(ctx, tc.tenantID, tc.docID)
			_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tc.tenantID).Delete()
			_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tc.tenantID).Delete()
		})
		chunks := splitter.SplitMarkdown(content)
		inputs := indexer.BuildChunkInputs(chunks, tc.tenantID, tc.docID, tc.source, "tenant", domain.SecretLevelInternal)
		_ = idx.DeleteByDocID(ctx, tc.tenantID, tc.docID)
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
	_ = idx.DeleteByDocID(ctx, tenantID, docID)
	if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
		TenantID: tenantID, DocID: docID, Name: "secret.md", SourceURI: source,
		MimeType: "text/markdown", Visibility: "tenant", SecretLevel: domain.SecretLevelSensitive, Status: "active", CreatedBy: "integration-test",
	}); err != nil {
		t.Fatalf("create document: %v", err)
	}
	t.Cleanup(func() {
		_ = idx.DeleteByDocID(ctx, tenantID, docID)
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
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

func TestDeleteLegacyByDocIDIntegration(t *testing.T) {
	svc, _, idx, cleanup := setupRAG(t)
	defer cleanup()

	ctx := gctx.New()
	tenantID := "it-legacy-" + uuid.NewString()[:8]
	docID := uuid.NewString()
	source := filepath.Join(t.TempDir(), "legacy.md")
	content := "# Legacy runbook\n\nLegacy-only cleanup verification phrase."
	if err := repository.NewDocumentRepo().Create(ctx, &repository.Document{
		TenantID: tenantID, DocID: docID, Name: "legacy.md", SourceURI: source,
		MimeType: "text/markdown", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "integration-test",
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = idx.DeleteByDocID(ctx, tenantID, docID)
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	inputs := indexer.BuildChunkInputs(splitter.SplitMarkdown(content), tenantID, docID, source, "tenant", domain.SecretLevelInternal)
	if _, err := idx.IndexChunks(ctx, inputs); err != nil {
		t.Fatalf("index legacy vectors: %v", err)
	}
	before, err := svc.Retrieve(ctx, &domain.RetrieveRequest{TenantID: tenantID, Query: "Legacy-only cleanup verification phrase", TopK: 3, MaxSecretLevel: domain.SecretLevelInternal})
	if err != nil || len(before.Documents) == 0 {
		t.Fatalf("retrieve legacy before delete = %#v, %v", before, err)
	}
	if err := idx.DeleteLegacyByDocID(ctx, tenantID, docID); err != nil {
		t.Fatalf("delete legacy vectors: %v", err)
	}
	after, err := svc.Retrieve(ctx, &domain.RetrieveRequest{TenantID: tenantID, Query: "Legacy-only cleanup verification phrase", TopK: 3, MaxSecretLevel: domain.SecretLevelInternal})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Documents) != 0 {
		t.Fatalf("legacy retrieval after delete = %#v, want no hits", after.Documents)
	}
}
