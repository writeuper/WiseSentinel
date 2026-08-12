//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"wisesentinel-platform/internal/domain"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestDocumentIndexGenerationFencesSupersededPublisherIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "index-state-itest-" + uuid.NewString()
	docID := "doc-" + uuid.NewString()
	documents := NewDocumentRepo()
	states := NewDocumentIndexStateRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	if err := documents.Create(ctx, &Document{
		TenantID: tenantID, DocID: docID, Name: "generation.md", SourceURI: "generation.md",
		Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "test",
	}); err != nil {
		t.Fatalf("create document: %v", err)
	}

	first := &IndexTaskRecord{
		TenantID: tenantID, TaskID: "task-" + uuid.NewString(), DocID: docID, SourceURI: "generation.md",
		Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Layer: domain.KnowledgeLayerStatic,
		Status: string(domain.IndexTaskPending),
	}
	if err := states.AllocateAndCreateTask(ctx, first); err != nil {
		t.Fatalf("allocate first generation: %v", err)
	}
	if first.Generation != 1 {
		t.Fatalf("first generation = %d, want 1", first.Generation)
	}
	if claimed, err := NewIndexTaskRepo().ClaimRunnable(ctx, tenantID, first.TaskID, "token-first", time.Now().Add(time.Hour)); err != nil || !claimed {
		t.Fatalf("claim first = %v, %v", claimed, err)
	}

	second := &IndexTaskRecord{
		TenantID: tenantID, TaskID: "task-" + uuid.NewString(), DocID: docID, SourceURI: "generation.md",
		Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Layer: domain.KnowledgeLayerStatic,
		Status: string(domain.IndexTaskPending),
	}
	if err := states.AllocateAndCreateTask(ctx, second); err != nil {
		t.Fatalf("allocate second generation: %v", err)
	}
	if second.Generation != 2 {
		t.Fatalf("second generation = %d, want 2", second.Generation)
	}

	published, err := states.PublishIfOwned(ctx, tenantID, first.TaskID, "token-first", 7)
	if err != nil || published {
		t.Fatalf("superseded first publish = %v, %v; want false, nil", published, err)
	}
	state, err := states.Get(ctx, tenantID, docID)
	if err != nil || state == nil || state.ActiveGeneration != 0 || state.DesiredGeneration != 2 {
		t.Fatalf("state after superseded publish = %#v, %v", state, err)
	}

	if claimed, err := NewIndexTaskRepo().ClaimRunnable(ctx, tenantID, second.TaskID, "token-second", time.Now().Add(time.Hour)); err != nil || !claimed {
		t.Fatalf("claim second = %v, %v", claimed, err)
	}
	published, err = states.PublishIfOwned(ctx, tenantID, second.TaskID, "token-second", 9)
	if err != nil || !published {
		t.Fatalf("current generation publish = %v, %v; want true, nil", published, err)
	}
	state, err = states.Get(ctx, tenantID, docID)
	if err != nil || state == nil || state.ActiveGeneration != 2 || state.ActiveTaskID != second.TaskID || state.LegacyAllowed {
		t.Fatalf("published state = %#v, %v", state, err)
	}
	stored, err := NewIndexTaskRepo().Get(ctx, tenantID, second.TaskID)
	if err != nil || stored == nil || stored.Status != string(domain.IndexTaskSuccess) || stored.ExecutionToken != "" || stored.ChunkCount != 9 {
		t.Fatalf("published task = %#v, %v", stored, err)
	}
}

func TestActiveRAGInventoryExcludesDeletedAndSupersededGenerationsIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "rag-inventory-" + uuid.NewString()
	indexedDoc, legacyDoc, deletedDoc := "doc-indexed-"+uuid.NewString(), "doc-legacy-"+uuid.NewString(), "doc-deleted-"+uuid.NewString()
	documents := NewDocumentRepo()
	before, err := documents.ActiveRAGInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	for _, document := range []*Document{
		{TenantID: tenantID, DocID: indexedDoc, Name: "indexed.md", SourceURI: "indexed.md", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "test"},
		{TenantID: tenantID, DocID: legacyDoc, Name: "legacy.md", SourceURI: "legacy.md", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "test"},
		{TenantID: tenantID, DocID: deletedDoc, Name: "deleted.md", SourceURI: "deleted.md", Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "deleted", CreatedBy: "test"},
	} {
		if err := documents.Create(ctx, document); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []g.Map{
		{"tenant_id": tenantID, "doc_id": indexedDoc, "active_generation": 2},
		{"tenant_id": tenantID, "doc_id": deletedDoc, "active_generation": 1},
	} {
		if _, err := g.DB().Ctx(ctx).Model("ws_document_index_state").Data(state).Insert(); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []g.Map{
		{"tenant_id": tenantID, "task_id": "task-" + uuid.NewString(), "doc_id": indexedDoc, "status": "success", "generation": 2, "chunk_count": 7},
		{"tenant_id": tenantID, "task_id": "task-" + uuid.NewString(), "doc_id": indexedDoc, "status": "success", "generation": 1, "chunk_count": 99},
		{"tenant_id": tenantID, "task_id": "task-" + uuid.NewString(), "doc_id": deletedDoc, "status": "success", "generation": 1, "chunk_count": 11},
	} {
		if _, err := g.DB().Ctx(ctx).Model("ws_index_task").Data(task).Insert(); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := documents.ActiveRAGInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.ActiveDocuments != before.ActiveDocuments+2 || inventory.ActivePublishedChunks != before.ActivePublishedChunks+7 || inventory.ActiveLegacyDocuments != before.ActiveLegacyDocuments+1 {
		t.Fatalf("inventory delta = %#v before %#v", inventory, before)
	}
}

func TestResolveActiveGenerationsExcludesDeletedDocumentsIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "index-resolve-itest-" + uuid.NewString()
	activeDocID := "doc-active-" + uuid.NewString()
	deletedDocID := "doc-deleted-" + uuid.NewString()
	documents := NewDocumentRepo()
	states := NewDocumentIndexStateRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	for _, docID := range []string{activeDocID, deletedDocID} {
		if err := documents.Create(ctx, &Document{
			TenantID: tenantID, DocID: docID, Name: docID, SourceURI: docID + ".md",
			Visibility: "tenant", SecretLevel: domain.SecretLevelInternal, Status: "active", CreatedBy: "test",
		}); err != nil {
			t.Fatalf("create %s: %v", docID, err)
		}
	}
	if err := documents.SoftDelete(ctx, tenantID, deletedDocID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	for _, docID := range []string{activeDocID, deletedDocID} {
		if _, err := g.DB().Ctx(ctx).Model("ws_document_index_state").Data(g.Map{
			"tenant_id": tenantID, "doc_id": docID, "active_generation": 3, "legacy_allowed": false,
		}).Insert(); err != nil {
			t.Fatalf("insert state for %s: %v", docID, err)
		}
	}
	resolved, err := states.ResolveActiveGenerations(ctx, tenantID, []string{activeDocID, deletedDocID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got, ok := resolved[activeDocID]; !ok || got.ActiveGeneration != 3 || got.LegacyAllowed {
		t.Fatalf("active document resolution = %#v, %v", got, ok)
	}
	if _, ok := resolved[deletedDocID]; ok {
		t.Fatalf("deleted document must not resolve as readable: %#v", resolved)
	}
}

func TestPublishEnqueuesDeduplicatedVectorGCIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "index-gc-itest-" + uuid.NewString()
	docID := "doc-" + uuid.NewString()
	documents := NewDocumentRepo()
	states := NewDocumentIndexStateRepo()
	tasks := NewIndexTaskRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document_index_state").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	if err := documents.Create(ctx, &Document{TenantID: tenantID, DocID: docID, Name: "gc.md", SourceURI: "gc.md", Visibility: "tenant", SecretLevel: 1, Status: "active", CreatedBy: "test"}); err != nil {
		t.Fatalf("create document: %v", err)
	}
	for generation := 1; generation <= 2; generation++ {
		task := &IndexTaskRecord{TenantID: tenantID, TaskID: "task-" + uuid.NewString(), DocID: docID, SourceURI: "gc.md", Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending)}
		if err := states.AllocateAndCreateTask(ctx, task); err != nil {
			t.Fatalf("allocate generation %d: %v", generation, err)
		}
		if task.Generation != uint64(generation) {
			t.Fatalf("generation = %d, want %d", task.Generation, generation)
		}
		token := fmt.Sprintf("token-%d", generation)
		if claimed, err := tasks.ClaimRunnable(ctx, tenantID, task.TaskID, token, time.Now().Add(time.Hour)); err != nil || !claimed {
			t.Fatalf("claim generation %d = %v, %v", generation, claimed, err)
		}
		if published, err := states.PublishIfOwned(ctx, tenantID, task.TaskID, token, generation); err != nil || !published {
			t.Fatalf("publish generation %d = %v, %v", generation, published, err)
		}
	}
	var rows []struct {
		TargetKey string `json:"target_key"`
		Status    string `json:"status"`
	}
	if err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Order("target_key ASC").Scan(&rows); err != nil {
		t.Fatalf("list gc outbox: %v", err)
	}
	if len(rows) != 2 || rows[0].TargetKey != "generation:1" || rows[1].TargetKey != "legacy:v1" || rows[0].Status != "pending" || rows[1].Status != "pending" {
		t.Fatalf("GC rows = %#v", rows)
	}
}

func TestVectorGCLeaseFencesStaleOwnerIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, docID := "gc-lease-"+uuid.NewString(), "doc-"+uuid.NewString()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Delete() })
	if _, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Data(g.Map{"tenant_id": tenantID, "doc_id": docID, "target_key": "generation:1", "target_kind": "generation", "target_generation": 1, "status": "pending"}).Insert(); err != nil {
		t.Fatal(err)
	}
	if ok, err := ClaimVectorGC(ctx, tenantID, docID, "generation:1", "token-a", time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("claim A = %v, %v", ok, err)
	}
	if _, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Data(g.Map{"lease_expires_at": time.Now().Add(-time.Minute)}).Update(); err != nil {
		t.Fatal(err)
	}
	if ok, err := ClaimVectorGC(ctx, tenantID, docID, "generation:1", "token-b", time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("claim B = %v, %v", ok, err)
	}
	if ok, err := FinishVectorGCIfOwned(ctx, tenantID, docID, "generation:1", "token-a", "succeeded", ""); err != nil || ok {
		t.Fatalf("stale finish = %v, %v", ok, err)
	}
	if ok, err := FinishVectorGCIfOwned(ctx, tenantID, docID, "generation:1", "token-b", "succeeded", ""); err != nil || !ok {
		t.Fatalf("owner finish = %v, %v", ok, err)
	}
}

func TestVectorGCRetryBackoffAndDeadLetterIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, docID := "gc-retry-"+uuid.NewString(), "doc-"+uuid.NewString()
	repo := NewVectorGCRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Delete() })
	if _, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Data(g.Map{
		"tenant_id": tenantID, "doc_id": docID, "target_key": "generation:1", "target_kind": "generation", "target_generation": 1,
		"status": "pending", "max_attempts": 2,
	}).Insert(); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt == 2 {
			if _, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
				Where("tenant_id", tenantID).Where("doc_id", docID).
				Data(g.Map{"next_attempt_at": time.Now().Add(-time.Second)}).Update(); err != nil {
				t.Fatalf("make retry runnable: %v", err)
			}
		}
		token := fmt.Sprintf("retry-token-%d", attempt)
		if ok, err := repo.Claim(ctx, tenantID, docID, "generation:1", token, time.Now().Add(time.Minute)); err != nil || !ok {
			t.Fatalf("claim %d = %v, %v", attempt, ok, err)
		}
		task, err := repo.Get(ctx, tenantID, docID, "generation:1")
		if err != nil || task == nil || task.AttemptCount != attempt {
			t.Fatalf("claimed row %d = %#v, %v", attempt, task, err)
		}
		if ok, err := repo.RetryIfOwned(ctx, task, token, time.Now().Add(time.Minute), "upstream token=secret"); err != nil || !ok {
			t.Fatalf("retry %d = %v, %v", attempt, ok, err)
		}
		stored, err := repo.Get(ctx, tenantID, docID, "generation:1")
		if err != nil || stored == nil {
			t.Fatalf("stored %d = %#v, %v", attempt, stored, err)
		}
		if attempt == 1 && (stored.Status != "retry_wait" || stored.NextAttemptAt == nil || stored.ExecutionToken != "") {
			t.Fatalf("first retry = %#v", stored)
		}
		if attempt == 2 && (stored.Status != "dead" || stored.NextAttemptAt != nil || stored.ExecutionToken != "") {
			t.Fatalf("dead letter = %#v", stored)
		}
	}
}

func TestSoftDeleteEnqueuesDocumentAllGCAtomicallyIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, docID := "gc-delete-"+uuid.NewString(), "doc-"+uuid.NewString()
	documents := NewDocumentRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	if err := documents.Create(ctx, &Document{TenantID: tenantID, DocID: docID, Name: "delete.md", SourceURI: "delete.md", Visibility: "tenant", SecretLevel: 1, Status: "active", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := documents.SoftDeleteAndEnqueueVectorGC(ctx, tenantID, docID); err != nil {
		t.Fatalf("soft delete with outbox: %v", err)
	}
	doc, err := documents.Get(ctx, tenantID, docID)
	if err != nil || doc == nil || doc.Status != "deleted" {
		t.Fatalf("deleted document = %#v, %v", doc, err)
	}
	task, err := NewVectorGCRepo().Get(ctx, tenantID, docID, "document:all")
	if err != nil || task == nil || task.TargetKind != VectorGCTargetDocumentAll || task.Status != "pending" {
		t.Fatalf("document_all outbox = %#v, %v", task, err)
	}
}

func TestSoftDeleteAtomicallyCancelsRunnableIndexTasksIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, docID := "gc-delete-tasks-"+uuid.NewString(), "doc-"+uuid.NewString()
	documents := NewDocumentRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_document").Where("tenant_id", tenantID).Delete()
	})
	if err := documents.Create(ctx, &Document{TenantID: tenantID, DocID: docID, Name: "delete-with-task.md", SourceURI: "delete-with-task.md", Visibility: "tenant", SecretLevel: 1, Status: "active", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	taskID := "task-" + uuid.NewString()
	if _, err := g.DB().Ctx(ctx).Model("ws_index_task").Data(g.Map{
		"tenant_id": tenantID, "task_id": taskID, "doc_id": docID, "source_uri": "delete-with-task.md",
		"visibility": "tenant", "secret_level": 1, "layer": "static", "status": "running", "generation": 1,
		"execution_token": "live-token", "lease_expires_at": time.Now().Add(time.Hour),
	}).Insert(); err != nil {
		t.Fatal(err)
	}
	if err := documents.SoftDeleteAndEnqueueVectorGC(ctx, tenantID, docID); err != nil {
		t.Fatalf("soft delete with runnable task: %v", err)
	}
	doc, err := documents.Get(ctx, tenantID, docID)
	if err != nil || doc == nil || doc.Status != "deleted" {
		t.Fatalf("document lifecycle = %#v, %v", doc, err)
	}
	indexed, err := NewIndexTaskRepo().Get(ctx, tenantID, taskID)
	if err != nil || indexed == nil || indexed.Status != "failed" || indexed.ExecutionToken != "" || indexed.LeaseExpiresAt != nil {
		t.Fatalf("cancelled index task = %#v, %v", indexed, err)
	}
	gc, err := NewVectorGCRepo().Get(ctx, tenantID, docID, "document:all")
	if err != nil || gc == nil || gc.Status != "pending" {
		t.Fatalf("document_all GC intent = %#v, %v", gc, err)
	}
}

func TestVectorGCRedriveRequiresDifferentApproverAndAtomicallyRequeuesIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, docID := "gc-redrive-"+uuid.NewString(), "doc-"+uuid.NewString()
	targetKey := "generation:9"
	repo := NewVectorGCRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_approval").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID).Delete()
	})
	if _, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Data(g.Map{
		"tenant_id": tenantID, "doc_id": docID, "target_key": targetKey, "target_kind": "generation", "target_generation": 9,
		"status": "dead", "attempt_count": 8, "max_attempts": 8, "last_error": "upstream secret=must-not-leak",
	}).Insert(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"doc_id": docID, "target_key": targetKey, "requested_by": "admin-a"})
	if err != nil {
		t.Fatal(err)
	}
	approvalID := "approval-" + uuid.NewString()
	gotID, created, err := repo.RequestRedriveApproval(ctx, tenantID, docID, targetKey, "admin-a", approvalID, string(payload), time.Now().Add(time.Hour))
	if err != nil || !created || gotID != approvalID {
		t.Fatalf("request redrive = id=%q created=%v err=%v", gotID, created, err)
	}
	duplicateID, created, err := repo.RequestRedriveApproval(ctx, tenantID, docID, targetKey, "admin-a", "approval-"+uuid.NewString(), string(payload), time.Now().Add(time.Hour))
	if err != nil || created || duplicateID != approvalID {
		t.Fatalf("duplicate request = id=%q created=%v err=%v", duplicateID, created, err)
	}
	if approved, err := repo.ApproveRedrive(ctx, tenantID, approvalID, "admin-a"); err == nil || approved {
		t.Fatalf("self approval = %v, %v; want false, error", approved, err)
	}
	if approved, err := repo.ApproveRedrive(ctx, tenantID, approvalID, "admin-b"); err != nil || !approved {
		t.Fatalf("different approval = %v, %v", approved, err)
	}
	task, err := repo.Get(ctx, tenantID, docID, targetKey)
	if err != nil || task == nil || task.Status != "pending" || task.AttemptCount != 0 || task.ExecutionToken != "" || task.LastError != "manual redrive approved" {
		t.Fatalf("requeued task = %#v, %v", task, err)
	}
	var approval Approval
	if err := g.DB().Ctx(ctx).Model("ws_approval").Where("tenant_id", tenantID).Where("approval_id", approvalID).Scan(&approval); err != nil || approval.Status != "approved" || approval.ApproverID != "admin-b" {
		t.Fatalf("approved record = %#v, %v", approval, err)
	}
	if approved, err := repo.ApproveRedrive(ctx, tenantID, approvalID, "admin-c"); err != nil || approved {
		t.Fatalf("replay approval = %v, %v; want false, nil", approved, err)
	}
}
