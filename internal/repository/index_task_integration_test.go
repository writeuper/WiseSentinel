//go:build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"wisesentinel-platform/internal/domain"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestIndexTaskLeaseCASAndFencedFinishIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "index-itest-" + uuid.NewString()
	taskID := "task-" + uuid.NewString()
	repo := NewIndexTaskRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete()
	})
	if err := repo.Create(ctx, &IndexTaskRecord{
		TenantID: tenantID, TaskID: taskID, DocID: "doc-" + uuid.NewString(), SourceURI: "integration.md",
		Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending),
	}); err != nil {
		t.Fatalf("create index task: %v", err)
	}
	claimed, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token-a", time.Now().Add(time.Hour))
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = repo.ClaimRunnable(ctx, tenantID, taskID, "token-b", time.Now().Add(time.Hour))
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v; want false, nil", claimed, err)
	}
	if _, err := g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Where("task_id", taskID).
		Data(g.Map{"lease_expires_at": time.Now().Add(-time.Minute)}).Update(); err != nil {
		t.Fatalf("expire running lease: %v", err)
	}
	claimed, err = repo.ClaimRunnable(ctx, tenantID, taskID, "token-b", time.Now().Add(time.Hour))
	if err != nil || !claimed {
		t.Fatalf("stale claim = %v, %v; want true, nil", claimed, err)
	}
	updated, err := repo.MarkFinishedIfOwned(ctx, tenantID, taskID, "token-a", string(domain.IndexTaskSuccess), 1, "")
	if err != nil || updated {
		t.Fatalf("old owner finish = %v, %v; want false, nil", updated, err)
	}
	updated, err = repo.MarkFinishedIfOwned(ctx, tenantID, taskID, "token-b", string(domain.IndexTaskSuccess), 2, "")
	if err != nil || !updated {
		t.Fatalf("current owner finish = %v, %v; want true, nil", updated, err)
	}
	task, err := repo.Get(ctx, tenantID, taskID)
	if err != nil || task == nil || task.Status != string(domain.IndexTaskSuccess) || task.ChunkCount != 2 || task.ExecutionToken != "" {
		t.Fatalf("terminal task = %#v, err=%v", task, err)
	}
}

func TestIndexTaskRenewLeaseFencesStaleOwnerIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, taskID := "index-renew-"+uuid.NewString(), "task-"+uuid.NewString()
	repo := NewIndexTaskRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &IndexTaskRecord{TenantID: tenantID, TaskID: taskID, DocID: "doc-" + uuid.NewString(), SourceURI: "renew.md", Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending)}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token-a", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("claim A = %v, %v", ok, err)
	}
	if _, err := g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Data(g.Map{"lease_expires_at": time.Now().Add(-time.Minute)}).Update(); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token-b", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("claim B = %v, %v", ok, err)
	}
	if ok, err := repo.RenewLeaseIfOwned(ctx, tenantID, taskID, "token-a", time.Now().Add(time.Hour)); err != nil || ok {
		t.Fatalf("stale renew = %v, %v", ok, err)
	}
	if ok, err := repo.RenewLeaseIfOwned(ctx, tenantID, taskID, "token-b", time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("owner renew = %v, %v", ok, err)
	}
}
