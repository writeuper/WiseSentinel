//go:build integration

package task

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/repository"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type retryingIndexExecutor struct {
	repo  *repository.IndexTaskRepo
	count int
}

func (e *retryingIndexExecutor) ExecuteIndexTask(ctx context.Context, tenantID, taskID, token string) error {
	e.count++
	if e.count == 1 {
		return errors.New("milvus rpc DeadlineExceeded")
	}
	_, err := e.repo.MarkFinishedIfOwned(ctx, tenantID, taskID, token, string(domain.IndexTaskSuccess), 1, "")
	return err
}

func TestIndexWorkerRetriesTransientFailureAndThenSucceedsIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, taskID := "index-retry-worker-"+uuid.NewString(), "task-"+uuid.NewString()
	repo := repository.NewIndexTaskRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &repository.IndexTaskRecord{TenantID: tenantID, TaskID: taskID, DocID: "doc-" + uuid.NewString(), SourceURI: "retry.md", Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending), MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	executor := &retryingIndexExecutor{repo: repo}
	worker := NewIndexWorker(repo, nil, executor)
	first, err := repo.Get(ctx, tenantID, taskID)
	if err != nil || first == nil {
		t.Fatalf("get initial task: %#v, %v", first, err)
	}
	worker.dispatch(ctx, first)
	deadline := time.Now().Add(5 * time.Second)
	var retry *repository.IndexTaskRecord
	for time.Now().Before(deadline) {
		retry, err = repo.Get(ctx, tenantID, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if retry != nil && retry.Status == string(domain.IndexTaskRetryWait) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if retry == nil || retry.Status != string(domain.IndexTaskRetryWait) || retry.AttemptCount != 1 || retry.NextAttemptAt == nil {
		t.Fatalf("after transient failure = %#v, want retry_wait attempt 1", retry)
	}
	// Make the durable retry runnable immediately for a deterministic test.
	if _, err := g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Where("task_id", taskID).Data(g.Map{"next_attempt_at": time.Now().Add(-time.Second)}).Update(); err != nil {
		t.Fatal(err)
	}
	retry, _ = repo.Get(ctx, tenantID, taskID)
	worker.dispatch(ctx, retry)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		retry, err = repo.Get(ctx, tenantID, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if retry != nil && retry.Status == string(domain.IndexTaskSuccess) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if retry == nil || retry.Status != string(domain.IndexTaskSuccess) || executor.count != 2 {
		t.Fatalf("after retry = %#v executor_count=%d, want success/2", retry, executor.count)
	}
}

func TestIndexLeaseDoesNotReleaseAnotherOwnerIntegration(t *testing.T) {
	addr := os.Getenv("OPS_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("OPS_TEST_REDIS_ADDR is not configured")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	key := "index-lease-itest:" + uuid.NewString()
	t.Cleanup(func() { _ = client.Del(ctx, key).Err() })
	if ok, err := client.SetNX(ctx, key, "token-a", 50*time.Millisecond).Result(); err != nil || !ok {
		t.Fatalf("owner A acquire = %v, %v", ok, err)
	}
	time.Sleep(80 * time.Millisecond)
	if ok, err := client.SetNX(ctx, key, "token-b", time.Second).Result(); err != nil || !ok {
		t.Fatalf("owner B acquire = %v, %v", ok, err)
	}
	if released, err := client.Eval(ctx, compareDeleteLua, []string{key}, "token-a").Int(); err != nil || released != 0 {
		t.Fatalf("stale owner release = %d, %v; want 0, nil", released, err)
	}
	if renewed, err := client.Eval(ctx, compareExpireLua, []string{key}, "token-a", time.Second.Milliseconds()).Int(); err != nil || renewed != 0 {
		t.Fatalf("stale owner renew = %d, %v; want 0, nil", renewed, err)
	}
	if value, err := client.Get(ctx, key).Result(); err != nil || value != "token-b" {
		t.Fatalf("lease owner after stale operations = %q, %v; want token-b", value, err)
	}
}

type dbOnlyIndexExecutor struct {
	repo *repository.IndexTaskRepo
	done chan error
}

func (e *dbOnlyIndexExecutor) ExecuteIndexTask(ctx context.Context, tenantID, taskID, token string) error {
	task, err := e.repo.Get(ctx, tenantID, taskID)
	if err == nil && (task == nil || task.Status != string(domain.IndexTaskRunning) || task.ExecutionToken != token) {
		err = os.ErrPermission
	}
	if err == nil {
		_, err = e.repo.MarkFinishedIfOwned(ctx, tenantID, taskID, token, string(domain.IndexTaskSuccess), 1, "")
	}
	e.done <- err
	return err
}

func TestIndexWorkerExecutesWithMySQLLeaseWhenRedisUnavailableIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, taskID := "index-db-only-"+uuid.NewString(), "task-"+uuid.NewString()
	repo := repository.NewIndexTaskRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &repository.IndexTaskRecord{
		TenantID: tenantID, TaskID: taskID, DocID: "doc-" + uuid.NewString(), SourceURI: "test.md",
		Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending), Generation: 1,
	}); err != nil {
		t.Fatal(err)
	}
	executor := &dbOnlyIndexExecutor{repo: repo, done: make(chan error, 1)}
	worker := NewIndexWorker(repo, nil, executor)
	worker.dispatch(ctx, &repository.IndexTaskRecord{TenantID: tenantID, TaskID: taskID})
	select {
	case err := <-executor.done:
		if err != nil {
			t.Fatalf("DB-only executor: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DB-only IndexWorker did not execute task")
	}
	stored, err := repo.Get(ctx, tenantID, taskID)
	if err != nil || stored == nil || stored.Status != string(domain.IndexTaskSuccess) || stored.ExecutionToken != "" {
		t.Fatalf("stored task = %#v, %v", stored, err)
	}
}

func TestIndexWorkerRenewsDBLeaseBeforeOriginalExpiryIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, taskID := "index-renew-worker-"+uuid.NewString(), "task-"+uuid.NewString()
	repo := repository.NewIndexTaskRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &repository.IndexTaskRecord{TenantID: tenantID, TaskID: taskID, DocID: "doc-" + uuid.NewString(), SourceURI: "renew.md", Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending)}); err != nil {
		t.Fatal(err)
	}
	originalExpiry := time.Now().Add(1500 * time.Millisecond)
	if ok, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token-a", originalExpiry); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	worker := NewIndexWorker(repo, nil, nil)
	worker.leaseTTL = 2 * time.Second
	cancelledCtx, cancel := context.WithCancel(context.Background())
	stop := worker.startDBLeaseRenewal(cancel, tenantID, taskID, "token-a")
	defer stop()
	defer cancel()
	time.Sleep(2100 * time.Millisecond)
	select {
	case <-cancelledCtx.Done():
		t.Fatal("valid DB lease renewal unexpectedly canceled worker")
	default:
	}
	stored, err := repo.Get(ctx, tenantID, taskID)
	if err != nil || stored == nil || stored.LeaseExpiresAt == nil || !stored.LeaseExpiresAt.After(time.Now()) || !stored.LeaseExpiresAt.After(originalExpiry) {
		t.Fatalf("renewed lease = %#v, %v", stored, err)
	}
	if ok, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token-b", time.Now().Add(time.Minute)); err != nil || ok {
		t.Fatalf("claim during renewed lease = %v, %v; want false, nil", ok, err)
	}
}

func TestIndexWorkerCancelsWhenDBLeaseIsLostIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, taskID := "index-lost-worker-"+uuid.NewString(), "task-"+uuid.NewString()
	repo := repository.NewIndexTaskRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &repository.IndexTaskRecord{TenantID: tenantID, TaskID: taskID, DocID: "doc-" + uuid.NewString(), SourceURI: "lost.md", Visibility: "tenant", SecretLevel: 1, Layer: domain.KnowledgeLayerStatic, Status: string(domain.IndexTaskPending)}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimRunnable(ctx, tenantID, taskID, "token-a", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	worker := NewIndexWorker(repo, nil, nil)
	worker.leaseTTL = 2 * time.Second
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := worker.startDBLeaseRenewal(cancel, tenantID, taskID, "token-a")
	defer stop()
	// Simulate a reclaim between ticks. The stale worker must observe that its
	// token no longer owns the DB lease and cancel its work context.
	if _, err := g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", tenantID).Where("task_id", taskID).Data(g.Map{"execution_token": "token-b"}).Update(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runCtx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("lost DB lease did not cancel worker context")
	}
}
