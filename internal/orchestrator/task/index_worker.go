package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// IndexWorker polls pending knowledge index tasks and executes them asynchronously.
type IndexWorker struct {
	taskRepo   *repository.IndexTaskRepo
	redis      *redis.Client
	executor   domain.IndexTaskExecutor
	pollPeriod time.Duration
	lockTTL    time.Duration
	leaseTTL   time.Duration
}

func NewIndexWorker(taskRepo *repository.IndexTaskRepo, redis *redis.Client, executor domain.IndexTaskExecutor) *IndexWorker {
	return &IndexWorker{
		taskRepo:   taskRepo,
		redis:      redis,
		executor:   executor,
		pollPeriod: 5 * time.Second,
		lockTTL:    30 * time.Minute,
		leaseTTL:   2 * time.Minute,
	}
}

func (w *IndexWorker) Start(ctx context.Context) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				g.Log().Errorf(ctx, "IndexWorker panic recovered: %v", recovered)
			}
		}()
		ticker := time.NewTicker(w.pollPeriod)
		defer ticker.Stop()
		w.tick(ctx)
		for {
			select {
			case <-ctx.Done():
				g.Log().Info(ctx, "IndexWorker stopped")
				return
			case <-ticker.C:
				w.tick(ctx)
			}
		}
	}()
}

func (w *IndexWorker) tick(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	tasks, err := w.taskRepo.ListRunnable(ctx, 5, time.Now())
	if err != nil {
		g.Log().Errorf(ctx, "IndexWorker: list pending failed: %v", err)
		return
	}
	for _, task := range tasks {
		w.dispatch(ctx, task)
	}
}

func (w *IndexWorker) dispatch(ctx context.Context, task *repository.IndexTaskRecord) {
	executionToken := uuid.NewString()
	lockKey := ""
	lockHeld := false
	// Redis is an optional load-shedding optimization. The MySQL ClaimRunnable
	// CAS below is the correctness boundary, so an unavailable cache must not
	// prevent durable indexing from progressing.
	if w.redis != nil {
		lockKey = fmt.Sprintf("ws:lock:index:%s:%s", task.TenantID, task.TaskID)
		ok, err := w.redis.SetNX(ctx, lockKey, executionToken, w.lockTTL).Result()
		if err != nil {
			g.Log().Warningf(ctx, "IndexWorker: redis SetNX unavailable for %s; using DB lease only: %v", task.TaskID, err)
		} else if !ok {
			return
		} else {
			lockHeld = true
		}
	}
	claimed, err := w.taskRepo.ClaimRunnable(ctx, task.TenantID, task.TaskID, executionToken, time.Now().Add(w.leaseTTL))
	if err != nil {
		if lockHeld {
			w.releaseLock(context.Background(), lockKey, executionToken)
		}
		g.Log().Errorf(ctx, "IndexWorker: claim task failed for %s: %v", task.TaskID, err)
		return
	}
	if !claimed {
		if lockHeld {
			w.releaseLock(context.Background(), lockKey, executionToken)
		}
		return
	}

	go func(t repository.IndexTaskRecord, token string, redisLockKey string, redisLockHeld bool) {
		defer func() {
			if recovered := recover(); recovered != nil {
				g.Log().Errorf(ctx, "IndexWorker task panic recovered: task_id=%s panic=%v", t.TaskID, recovered)
				_, _ = w.taskRepo.MarkFinishedIfOwned(context.Background(), t.TenantID, t.TaskID, token, string(domain.IndexTaskFailed), 0, fmt.Sprintf("panic: %v", recovered))
			}
		}()
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		stopDBRenewal := w.startDBLeaseRenewal(cancel, t.TenantID, t.TaskID, token)
		defer stopDBRenewal()
		if redisLockHeld {
			defer w.releaseLock(context.Background(), redisLockKey, token)
			stopRenewal := w.startLockRenewal(cancel, redisLockKey, token)
			defer stopRenewal()
		}

		if err := w.executor.ExecuteIndexTask(runCtx, t.TenantID, t.TaskID, token); err != nil {
			g.Log().Errorf(runCtx, "IndexWorker: execute failed for %s: %v", t.TaskID, err)
			if isRetryableIndexError(err) {
				attempt := t.AttemptCount
				maxAttempts := t.MaxAttempts
				if maxAttempts <= 0 {
					maxAttempts = 3
				}
				if attempt < maxAttempts {
					delay := indexRetryDelay(attempt)
					if ok, retryErr := w.taskRepo.RetryIfOwned(context.Background(), &t, token, time.Now().Add(delay), err.Error()); retryErr != nil || !ok {
						g.Log().Warningf(runCtx, "IndexWorker: retry transition failed for %s: %v", t.TaskID, retryErr)
					}
					return
				}
			}
			_, _ = w.taskRepo.MarkFinishedIfOwned(context.Background(), t.TenantID, t.TaskID, token, string(domain.IndexTaskFailed), 0, err.Error())
			return
		}
		g.Log().Infof(runCtx, "IndexWorker: finished task %s", t.TaskID)
	}(*task, executionToken, lockKey, lockHeld)
}

func isRetryableIndexError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "document deleted") || strings.Contains(text, "http 403") || strings.Contains(text, "http 404") || strings.Contains(text, "allocationquota") {
		return false
	}
	return strings.Contains(text, "deadlineexceeded") || strings.Contains(text, "deadline exceeded") || strings.Contains(text, "timeout") || strings.Contains(text, "connection reset") || strings.Contains(text, "connection refused") || strings.Contains(text, "http 500") || strings.Contains(text, "http 502") || strings.Contains(text, "http 503") || strings.Contains(text, "http 504")
}

func indexRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	if delay > 2*time.Minute {
		return 2 * time.Minute
	}
	return delay
}

func (w *IndexWorker) startDBLeaseRenewal(cancelRun context.CancelFunc, tenantID, taskID, token string) func() {
	stop := make(chan struct{})
	interval := w.leaseTTL / 3
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
				ok, err := w.taskRepo.RenewLeaseIfOwned(context.Background(), tenantID, taskID, token, time.Now().Add(w.leaseTTL))
				if err != nil {
					g.Log().Errorf(context.Background(), "IndexWorker: DB lease renewal failed for %s: %v", taskID, err)
					cancelRun()
					return
				}
				if !ok {
					g.Log().Warningf(context.Background(), "IndexWorker: DB lease ownership lost for %s", taskID)
					cancelRun()
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

func (w *IndexWorker) startLockRenewal(cancelRun context.CancelFunc, key, token string) func() {
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
				if err != nil {
					g.Log().Errorf(context.Background(), "IndexWorker: Redis lock renewal failed for %s: %v", key, err)
					cancelRun()
					return
				}
				if result != 1 {
					g.Log().Warningf(context.Background(), "IndexWorker: Redis lock ownership lost for %s", key)
					cancelRun()
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

func (w *IndexWorker) releaseLock(ctx context.Context, key, token string) {
	_, _ = w.redis.Eval(ctx, compareDeleteLua, []string{key}, token).Result()
}
