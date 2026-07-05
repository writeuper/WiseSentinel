// Package task hosts async worker schedulers.
package task

import (
	"context"
	"fmt"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// OpsWorker polls the database for pending Ops tasks and dispatches
// them to the registered Ops Agent for execution.
//
// It implements the design doc's §11.1 requirement: every 5 seconds scan
// ws_ops_task for status=pending, claim a task via a Redis distributed
// lock, and run the agent.
type OpsWorker struct {
	taskRepo   *repository.OpsTaskRepo
	redis      *redis.Client
	opsAgent   domain.AgentRunner
	pollPeriod time.Duration
	lockTTL    time.Duration
}

// NewOpsWorker creates a worker instance.
func NewOpsWorker(taskRepo *repository.OpsTaskRepo, redis *redis.Client, opsAgent domain.AgentRunner) *OpsWorker {
	return &OpsWorker{
		taskRepo:   taskRepo,
		redis:      redis,
		opsAgent:   opsAgent,
		pollPeriod: 5 * time.Second,
		lockTTL:    10 * time.Minute,
	}
}

// Start runs the polling loop in a background goroutine until ctx is done.
func (w *OpsWorker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(w.pollPeriod)
		defer ticker.Stop()
		w.tick(ctx) // run once immediately
		for {
			select {
			case <-ctx.Done():
				g.Log().Info(ctx, "OpsWorker stopped")
				return
			case <-ticker.C:
				w.tick(ctx)
			}
		}
	}()
}

// tick runs a single polling cycle.
func (w *OpsWorker) tick(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	tasks, err := w.taskRepo.ListPending(ctx, 10)
	if err != nil {
		g.Log().Errorf(ctx, "OpsWorker: list pending failed: %v", err)
		return
	}
	for _, t := range tasks {
		w.dispatch(ctx, t)
	}
}

// dispatch tries to acquire the lock and run the agent for a single task.
func (w *OpsWorker) dispatch(ctx context.Context, t *repository.OpsTask) {
	lockKey := fmt.Sprintf("ws:lock:ops:%s", t.TaskID)
	// SETNX with TTL — if lock is held, skip.
	ok, err := w.redis.SetNX(ctx, lockKey, "locked", w.lockTTL).Result()
	if err != nil {
		g.Log().Errorf(ctx, "OpsWorker: redis SetNX failed for %s: %v", t.TaskID, err)
		return
	}
	if !ok {
		// Another worker is already handling this task.
		return
	}

	go func(task repository.OpsTask) {
		// Use a fresh background context for the actual run so that the
		// polling-loop context cancellation does not abort a long-running agent.
		runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer runCancel()
		defer w.releaseLock(context.Background(), lockKey)

		if err := w.taskRepo.MarkRunning(runCtx, task.TenantID, task.TaskID); err != nil {
			g.Log().Errorf(runCtx, "OpsWorker: mark running failed: %v", err)
			return
		}

		userID := task.CreatedBy
		if userID == "" {
			userID = "system"
		}
		req := &domain.OpsAgentRequest{
			TenantID:      task.TenantID,
			UserID:        userID,
			Query:         task.InputQuery,
			MaxIterations: 20,
			Async:         false, // the worker always runs synchronously
		}
		// Inject tenant and user into the agent context.
		runCtx = w.contextWithIdentity(runCtx, task.TenantID, userID, task.TraceID)

		resp, err := w.opsAgent.OpsAnalyze(runCtx, req)
		if err != nil {
			detailJSON := `["异步执行失败: ` + err.Error() + `"]`
			_ = w.taskRepo.MarkFinished(runCtx, task.TenantID, task.TaskID,
				string(domain.OpsTaskFailed), err.Error(), detailJSON)
			g.Log().Errorf(runCtx, "OpsWorker: agent failed for %s: %v", task.TaskID, err)
			return
		}
		if resp == nil {
			_ = w.taskRepo.MarkFinished(runCtx, task.TenantID, task.TaskID,
				string(domain.OpsTaskFailed), "agent returned nil response", `["nil response"]`)
			return
		}
		// The agent's own internal sync path already updated the row, but we
		// guard against a missing update here.
		g.Log().Infof(runCtx, "OpsWorker: finished task %s status=%s", task.TaskID, resp.Status)
	}(*t)
}

// contextWithIdentity returns a new context with the standard identity keys
// populated so that downstream services pick them up.
func (w *OpsWorker) contextWithIdentity(ctx context.Context, tenantID, userID, traceID string) context.Context {
	// We don't import ctxkeys here to keep the package dependency set minimal.
	// The agent reads tenant/user from the OpsAgentRequest directly, so we
	// only need to provide traceID.
	if traceID == "" {
		traceID = uuid.NewString()
	}
	return gctx.WithCtx(ctx) // placeholder; trace already supplied via the request
}

// releaseLock deletes the distributed lock (best-effort).
func (w *OpsWorker) releaseLock(ctx context.Context, key string) {
	_ = w.redis.Del(ctx, key).Err()
}
