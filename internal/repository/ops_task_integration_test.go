//go:build integration

package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"wisesentinel-platform/internal/domain"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestOpsTaskLeaseCASAndBoundedRetryIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "ops-itest-" + uuid.NewString()
	repo := NewOpsTaskRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_ops_task").Where("tenant_id", tenantID).Delete()
	})

	createTask := func(taskID string, maxRetry int) {
		t.Helper()
		if err := repo.Create(ctx, &OpsTask{
			TenantID: tenantID, TaskID: taskID, TriggerType: "manual", InputQuery: "integration test",
			Status: string(domain.OpsTaskPending), MaxRetry: maxRetry,
		}); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	taskFence := "fence-" + uuid.NewString()
	createTask(taskFence, 1)
	claimed, err := repo.ClaimRunnable(ctx, tenantID, taskFence, "token-a", time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = repo.ClaimRunnable(ctx, tenantID, taskFence, "token-b", time.Now().Add(time.Minute))
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v; want false, nil", claimed, err)
	}
	updated, err := repo.FinishIfOwned(ctx, tenantID, taskFence, "token-b", string(domain.OpsTaskSuccess), "stale", `{}`)
	if err != nil || updated {
		t.Fatalf("stale token finish = %v, %v; want false, nil", updated, err)
	}
	updated, err = repo.FinishIfOwned(ctx, tenantID, taskFence, "token-a", string(domain.OpsTaskSuccess), "owned", `{}`)
	if err != nil || !updated {
		t.Fatalf("owner finish = %v, %v; want true, nil", updated, err)
	}

	taskRetry := "retry-" + uuid.NewString()
	createTask(taskRetry, 1)
	claimed, err = repo.ClaimRunnable(ctx, tenantID, taskRetry, "retry-a", time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("retry task claim = %v, %v", claimed, err)
	}
	retried, changed, err := repo.RetryOrFailIfOwned(ctx, tenantID, taskRetry, "retry-a", "transient error", time.Now().Add(time.Hour))
	if err != nil || !retried || !changed {
		t.Fatalf("first retry = retried:%v changed:%v err:%v; want true,true,nil", retried, changed, err)
	}
	claimed, err = repo.ClaimRunnable(ctx, tenantID, taskRetry, "retry-b", time.Now().Add(time.Minute))
	if err != nil || claimed {
		t.Fatalf("early retry claim = %v, %v; want false, nil", claimed, err)
	}
	if _, err := g.DB().Ctx(ctx).Model("ws_ops_task").Where("tenant_id", tenantID).Where("task_id", taskRetry).
		Data(g.Map{"next_attempt_at": time.Now().Add(-time.Second)}).Update(); err != nil {
		t.Fatalf("make retry runnable: %v", err)
	}
	claimed, err = repo.ClaimRunnable(ctx, tenantID, taskRetry, "retry-b", time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("due retry claim = %v, %v; want true, nil", claimed, err)
	}
	updated, err = repo.FinishIfOwned(ctx, tenantID, taskRetry, "retry-a", string(domain.OpsTaskSuccess), "stale retry result", `{}`)
	if err != nil || updated {
		t.Fatalf("old retry owner finish = %v, %v; want false, nil", updated, err)
	}
	retried, changed, err = repo.RetryOrFailIfOwned(ctx, tenantID, taskRetry, "retry-b", "retry exhausted", time.Now())
	if err != nil || retried || !changed {
		t.Fatalf("exhausted retry = retried:%v changed:%v err:%v; want false,true,nil", retried, changed, err)
	}
	task, err := repo.Get(ctx, tenantID, taskRetry)
	if err != nil || task == nil || task.Status != string(domain.OpsTaskFailed) || task.RetryCount != 1 {
		t.Fatalf("terminal task = %#v, err=%v; want failed with retry_count=1", task, err)
	}
}

func TestOpsTaskPersistenceSuppressesTelemetryBodiesIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, taskID := "ops-telemetry-"+uuid.NewString(), "task-"+uuid.NewString()
	const canary = "WS_OPS_TELEMETRY_CANARY_1234567890"
	repo := NewOpsTaskRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_ops_task").Where("tenant_id", tenantID).Delete()
	})
	if err := repo.Create(ctx, &OpsTask{TenantID: tenantID, TaskID: taskID, TriggerType: "manual", InputQuery: "execute safely", Status: string(domain.OpsTaskPending)}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("claim = %v, %v; want true, nil", ok, err)
	}
	if ok, err := repo.FinishIfOwned(ctx, tenantID, taskID, "token", string(domain.OpsTaskSuccess), canary, `{"tool_output":"`+canary+`"}`); err != nil || !ok {
		t.Fatalf("finish = %v, %v; want true, nil", ok, err)
	}
	var row struct {
		Result     string `json:"result"`
		DetailJSON string `json:"detail_json"`
		LastError  string `json:"last_error"`
	}
	if err := g.DB().Ctx(ctx).Model("ws_ops_task").Where("tenant_id", tenantID).Where("task_id", taskID).Scan(&row); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{row.Result, row.DetailJSON, row.LastError} {
		if strings.Contains(value, canary) || (value != "" && !strings.Contains(value, `"suppressed"`)) {
			t.Fatalf("unsafe ops-task telemetry value: %q", value)
		}
	}
}
