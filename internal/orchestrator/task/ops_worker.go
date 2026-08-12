// Package task hosts async worker schedulers.
package task

import (
	"context"
	"fmt"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	opsTaskRunTimeout = 30 * time.Minute
	compareDeleteLua  = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`
	compareExpireLua  = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("PEXPIRE", KEYS[1], ARGV[2]) else return 0 end`
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
	opsAgent   domain.OpsAgent
	pollPeriod time.Duration
	lockTTL    time.Duration
}

// NewOpsWorker creates a worker instance.
func NewOpsWorker(taskRepo *repository.OpsTaskRepo, redis *redis.Client, opsAgent domain.OpsAgent) *OpsWorker {
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
		defer func() {
			if recovered := recover(); recovered != nil {
				g.Log().Errorf(ctx, "OpsWorker panic recovered: %v", recovered)
			}
		}()
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
	if _, err := w.taskRepo.ExpireTimedOut(ctx, time.Now()); err != nil {
		g.Log().Errorf(ctx, "OpsWorker: expire timed out tasks failed: %v", err)
	}

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
	lockKey := fmt.Sprintf("ws:lock:ops:%s:%s", t.TenantID, t.TaskID)
	executionToken := uuid.NewString()
	// SETNX with TTL — if lock is held, skip.
	ok, err := w.redis.SetNX(ctx, lockKey, executionToken, w.lockTTL).Result()
	if err != nil {
		g.Log().Errorf(ctx, "OpsWorker: redis SetNX failed for %s: %v", t.TaskID, err)
		return
	}
	if !ok {
		// Another worker is already handling this task.
		return
	}
	claimed, err := w.taskRepo.ClaimRunnable(ctx, t.TenantID, t.TaskID, executionToken, time.Now().Add(opsTaskRunTimeout))
	if err != nil {
		w.releaseLock(context.Background(), lockKey, executionToken)
		g.Log().Errorf(ctx, "OpsWorker: claim task failed for %s: %v", t.TaskID, err)
		return
	}
	if !claimed {
		w.releaseLock(context.Background(), lockKey, executionToken)
		return
	}

	go func(task repository.OpsTask, token string) {
		defer func() {
			if recovered := recover(); recovered != nil {
				g.Log().Errorf(ctx, "OpsWorker task panic recovered: task_id=%s panic=%v", task.TaskID, recovered)
				_, _ = w.taskRepo.FinishIfOwned(context.Background(), task.TenantID, task.TaskID, token,
					string(domain.OpsTaskFailed), fmt.Sprintf("worker panic: %v", recovered), `[]`)
			}
		}()
		// Use a fresh background context for the actual run so that the
		// polling-loop context cancellation does not abort a long-running agent.
		runCtx, runCancel := context.WithTimeout(context.Background(), opsTaskRunTimeout)
		defer runCancel()
		defer w.releaseLock(context.Background(), lockKey, token)
		stopRenewal := w.startLockRenewal(runCancel, lockKey, token)
		defer stopRenewal()

		userID := task.CreatedBy
		if userID == "" {
			userID = "system"
		}
		// Inject tenant and user into the agent context.
		runCtx = w.contextWithIdentity(runCtx, task.TenantID, userID, task.TraceID)

		resp, err := w.opsAgent.ExecuteTask(runCtx, task.TenantID, task.TaskID, token)
		if err != nil {
			g.Log().Errorf(runCtx, "OpsWorker: agent failed for %s: %v", task.TaskID, err)
			return
		}
		if resp == nil {
			_, _ = w.taskRepo.FinishIfOwned(runCtx, task.TenantID, task.TaskID, token,
				string(domain.OpsTaskFailed), "agent returned nil response", `["nil response"]`)
			return
		}
		g.Log().Infof(runCtx, "OpsWorker: finished task %s status=%s", task.TaskID, resp.Status)
	}(*t, executionToken)
}

// contextWithIdentity returns a new context with the standard identity keys
// populated so that downstream services pick them up.
func (w *OpsWorker) contextWithIdentity(ctx context.Context, tenantID, userID, traceID string) context.Context {
	if traceID == "" {
		traceID = uuid.NewString()
	}
	ctx = ctxkeys.WithTenantID(ctx, tenantID)
	ctx = ctxkeys.WithUserID(ctx, userID)
	return ctxkeys.WithTraceID(ctx, traceID)
}

// startLockRenewal keeps a token-owned Redis lease alive throughout agent
// execution. A lost lease cancels the run; terminal DB writes remain fenced by
// the execution token even if the old goroutine returns late.
func (w *OpsWorker) startLockRenewal(cancelRun context.CancelFunc, key, token string) func() {
	stop := make(chan struct{})
	interval := w.lockTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				result, err := w.redis.Eval(context.Background(), compareExpireLua, []string{key}, token, w.lockTTL.Milliseconds()).Int()
				if err != nil || result != 1 {
					g.Log().Errorf(context.Background(), "OpsWorker: lock lease lost for %s: %v", key, err)
					cancelRun()
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

// releaseLock releases only the caller's own Redis lease (best-effort).
func (w *OpsWorker) releaseLock(ctx context.Context, key, token string) {
	_, _ = w.redis.Eval(ctx, compareDeleteLua, []string{key}, token).Result()
}
