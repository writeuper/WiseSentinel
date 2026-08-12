//go:build integration

package repository

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestApprovalExpiryAndDecisionCASIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "approval-itest-" + uuid.NewString()
	repo := NewApprovalRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_approval").Where("tenant_id", tenantID).Delete()
	})
	create := func(id string, expiresAt time.Time) {
		t.Helper()
		if err := repo.Create(ctx, &Approval{TenantID: tenantID, ApprovalID: id, TaskID: "tool", ApprovalType: "tool_invoke", PayloadJSON: `{}`, Status: "pending", ExpiredAt: expiresAt}); err != nil {
			t.Fatalf("create approval: %v", err)
		}
	}

	expiredID := "expired-" + uuid.NewString()
	create(expiredID, time.Now().Add(-time.Minute))
	if approved, err := repo.Decide(ctx, tenantID, expiredID, "approved", "admin", "too late"); err != nil || approved {
		t.Fatalf("expired approval decision = %v, %v; want false, nil", approved, err)
	}
	if err := repo.ExpireStale(ctx); err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	items, total, err := repo.ListPending(ctx, tenantID, 1, 20)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("expired approval listed: items=%#v total=%d err=%v", items, total, err)
	}

	concurrentID := "concurrent-" + uuid.NewString()
	create(concurrentID, time.Now().Add(time.Hour))
	var winners atomic.Int32
	var wg sync.WaitGroup
	for _, decision := range []string{"approved", "rejected"} {
		wg.Add(1)
		go func(decision string) {
			defer wg.Done()
			ok, err := repo.Decide(ctx, tenantID, concurrentID, decision, "admin-"+decision, decision)
			if err != nil {
				t.Errorf("concurrent %s decision: %v", decision, err)
			}
			if ok {
				winners.Add(1)
			}
		}(decision)
	}
	wg.Wait()
	if got := winners.Load(); got != 1 {
		t.Fatalf("decision winners = %d, want exactly 1", got)
	}
}

func TestApprovalDecisionPersistsRedactedCommentIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, approvalID := "approval-redact-"+uuid.NewString(), "approval-"+uuid.NewString()
	repo := NewApprovalRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_approval").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &Approval{TenantID: tenantID, ApprovalID: approvalID, TaskID: "tool", ApprovalType: "tool_invoke", PayloadJSON: `{}`, Status: "pending", ExpiredAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	const canary = "WS_CANARY_APPROVAL_SECRET_1234567890"
	updated, err := repo.Decide(ctx, tenantID, approvalID, "rejected", "sre-admin", "investigated with Bearer "+canary)
	if err != nil || !updated {
		t.Fatalf("decide approval = %v, %v", updated, err)
	}
	var stored struct {
		Comment string `json:"comment"`
	}
	if err := g.DB().Ctx(ctx).Model("ws_approval").Where("tenant_id", tenantID).Where("approval_id", approvalID).Fields("comment").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.Comment, canary) {
		t.Fatalf("approval comment leaked canary into persistence: %q", stored.Comment)
	}
	if !strings.Contains(stored.Comment, "<redacted:authorization>") {
		t.Fatalf("approval comment lacks redaction marker: %q", stored.Comment)
	}
}

func TestApprovalGenericCannotBeApprovedWithoutDedicatedExecutorIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, approvalID := "appr-generic-"+uuid.NewString(), "approval-"+uuid.NewString()
	repo := NewApprovalRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_approval").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &Approval{TenantID: tenantID, ApprovalID: approvalID, TaskID: "tool", ApprovalType: "tool_invoke", PayloadJSON: `{}`, Status: "pending", ExpiredAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	approved, err := repo.Decide(ctx, tenantID, approvalID, "approved", "sre-admin", "approve")
	if err != nil || approved {
		t.Fatalf("generic approval = %v, %v; want false, nil", approved, err)
	}
	pending, err := repo.GetPending(ctx, tenantID, approvalID)
	if err != nil || pending == nil || pending.Status != "pending" {
		t.Fatalf("generic approval must remain pending: %#v, %v", pending, err)
	}
}
