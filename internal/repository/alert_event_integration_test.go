//go:build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestAlertEventReapOrphanReservationsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenant := "orphan-reap-" + uuid.NewString()
	orphanEvent := "event-" + uuid.NewString()
	validEvent := "event-" + uuid.NewString()
	validTask := "ops_" + uuid.NewString()
	now := time.Now().Add(-10 * time.Minute)
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_alert_event").Where("tenant_id", tenant).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_ops_task").Where("tenant_id", tenant).Delete()
	})
	for _, event := range []*AlertEvent{
		{TenantID: tenant, EventID: orphanEvent, IncidentKey: "orphan", Status: "firing", PayloadJSON: `{}`, TaskID: "ops_missing", ReceivedAt: now},
		{TenantID: tenant, EventID: validEvent, IncidentKey: "valid", Status: "firing", PayloadJSON: `{}`, TaskID: validTask, ReceivedAt: now},
	} {
		if err := NewAlertEventRepo().Create(ctx, event); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}
	if err := NewOpsTaskRepo().Create(ctx, &OpsTask{TenantID: tenant, TaskID: validTask, TriggerType: "webhook", InputQuery: "synthetic", Status: "pending", MaxRetry: 2}); err != nil {
		t.Fatalf("create valid task: %v", err)
	}
	reaped, err := NewAlertEventRepo().ReapOrphanReservations(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("reap orphan reservations: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want 1", reaped)
	}
	gotOrphan, err := NewAlertEventRepo().Get(ctx, tenant, orphanEvent)
	if err != nil || gotOrphan != nil {
		t.Fatalf("orphan event after reap = %#v, err=%v", gotOrphan, err)
	}
	gotValid, err := NewAlertEventRepo().Get(ctx, tenant, validEvent)
	if err != nil || gotValid == nil {
		t.Fatalf("valid event after reap = %#v, err=%v", gotValid, err)
	}
}
