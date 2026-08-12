package task

import (
	"context"
	"fmt"
	"time"

	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

const (
	vectorGCPollPeriod = 5 * time.Second
	vectorGCLeaseTTL   = 2 * time.Minute
	vectorGCRunTimeout = 90 * time.Second
)

type vectorGCTaskStore interface {
	ListRunnable(context.Context, int, time.Time) ([]*repository.VectorGCTask, error)
	Get(context.Context, string, string, string) (*repository.VectorGCTask, error)
	Claim(context.Context, string, string, string, string, time.Time) (bool, error)
	FinishIfOwned(context.Context, string, string, string, string, string, string) (bool, error)
	RetryIfOwned(context.Context, *repository.VectorGCTask, string, time.Time, string) (bool, error)
}

type vectorGCSafetyGate interface {
	CanDeleteGeneration(context.Context, string, string, uint64) (bool, error)
	CanDeleteLegacy(context.Context, string, string) (bool, error)
	CanDeleteDocumentAll(context.Context, string, string) (bool, error)
}

type vectorGCDeleter interface {
	DeleteByGeneration(context.Context, string, string, uint64) error
	DeleteLegacyByDocID(context.Context, string, string) error
	DeleteByDocID(context.Context, string, string) error
}

// VectorGCWorker consumes durable cleanup intents. MySQL leases and terminal
// CAS writes provide the correctness boundary; a process crash after Milvus
// delete is intentionally replay-safe because deletes are idempotent.
type VectorGCWorker struct {
	tasks      vectorGCTaskStore
	safety     vectorGCSafetyGate
	vectors    vectorGCDeleter
	pollPeriod time.Duration
	leaseTTL   time.Duration
	runTimeout time.Duration
}

func NewVectorGCWorker(tasks *repository.VectorGCRepo, safety *repository.DocumentIndexStateRepo, vectors *indexer.MilvusIndexer) *VectorGCWorker {
	return newVectorGCWorker(tasks, safety, vectors)
}

func newVectorGCWorker(tasks vectorGCTaskStore, safety vectorGCSafetyGate, vectors vectorGCDeleter) *VectorGCWorker {
	return &VectorGCWorker{tasks: tasks, safety: safety, vectors: vectors, pollPeriod: vectorGCPollPeriod, leaseTTL: vectorGCLeaseTTL, runTimeout: vectorGCRunTimeout}
}

func (w *VectorGCWorker) Start(ctx context.Context) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				g.Log().Errorf(ctx, "VectorGCWorker panic recovered: %v", recovered)
			}
		}()
		ticker := time.NewTicker(w.pollPeriod)
		defer ticker.Stop()
		w.tick(ctx)
		for {
			select {
			case <-ctx.Done():
				g.Log().Info(ctx, "VectorGCWorker stopped")
				return
			case <-ticker.C:
				w.tick(ctx)
			}
		}
	}()
}

func (w *VectorGCWorker) tick(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, w.runTimeout)
	defer cancel()
	tasks, err := w.tasks.ListRunnable(ctx, 10, time.Now())
	if err != nil {
		g.Log().Errorf(ctx, "VectorGCWorker: list runnable tasks failed: %s", redact.Summary(err.Error(), 500))
		return
	}
	if reporter, ok := w.tasks.(interface {
		CountByStatus(context.Context) (map[string]int, error)
	}); ok {
		if counts, err := reporter.CountByStatus(ctx); err != nil {
			g.Log().Warningf(ctx, "VectorGCWorker: queue metric snapshot failed: %s", redact.Summary(err.Error(), 500))
		} else {
			for _, status := range []string{"pending", "running", "retry_wait", "succeeded", "skipped", "dead"} {
				observability.SetVectorGCTasks(status, counts[status])
			}
		}
	}
	for _, task := range tasks {
		w.dispatch(ctx, task)
	}
}

func (w *VectorGCWorker) dispatch(parent context.Context, candidate *repository.VectorGCTask) {
	if candidate == nil {
		return
	}
	token := uuid.NewString()
	claimed, err := w.tasks.Claim(parent, candidate.TenantID, candidate.DocID, candidate.TargetKey, token, time.Now().Add(w.leaseTTL))
	if err != nil {
		g.Log().Errorf(parent, "VectorGCWorker: claim target=%s failed: %s", candidate.TargetKey, redact.Summary(err.Error(), 500))
		return
	}
	if !claimed {
		return
	}
	// Reload after Claim: the persisted attempt count determines retry/dead
	// behavior, and this token fences any stale worker's completion.
	task, err := w.tasks.Get(parent, candidate.TenantID, candidate.DocID, candidate.TargetKey)
	if err != nil || task == nil || task.ExecutionToken != token {
		if err != nil {
			g.Log().Errorf(parent, "VectorGCWorker: reload claimed target=%s failed: %s", candidate.TargetKey, redact.Summary(err.Error(), 500))
		}
		return
	}
	go w.run(task, token)
}

func (w *VectorGCWorker) run(task *repository.VectorGCTask, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), w.runTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			w.retry(ctx, task, token, fmt.Sprintf("worker panic: %v", recovered))
		}
	}()

	safe, err := w.safeToDelete(ctx, task)
	if err != nil {
		w.retry(ctx, task, token, err.Error())
		return
	}
	if !safe {
		// The read-side state now protects the target (for example it became the
		// active generation). This is a successful no-op, not a retryable error.
		if ok, _ := w.tasks.FinishIfOwned(ctx, task.TenantID, task.DocID, task.TargetKey, token, "skipped", "cleanup target is currently protected"); ok {
			observability.ObserveVectorGCAttempt(task.TargetKind, "skipped")
		}
		return
	}
	if err := w.delete(ctx, task); err != nil {
		w.retry(ctx, task, token, err.Error())
		return
	}
	if ok, err := w.tasks.FinishIfOwned(ctx, task.TenantID, task.DocID, task.TargetKey, token, "succeeded", ""); err != nil {
		g.Log().Errorf(ctx, "VectorGCWorker: finish target=%s failed: %s", task.TargetKey, redact.Summary(err.Error(), 500))
	} else if ok {
		observability.ObserveVectorGCAttempt(task.TargetKind, "succeeded")
	}
}

func (w *VectorGCWorker) safeToDelete(ctx context.Context, task *repository.VectorGCTask) (bool, error) {
	switch task.TargetKind {
	case repository.VectorGCTargetGeneration:
		return w.safety.CanDeleteGeneration(ctx, task.TenantID, task.DocID, task.TargetGeneration)
	case repository.VectorGCTargetLegacy:
		return w.safety.CanDeleteLegacy(ctx, task.TenantID, task.DocID)
	case repository.VectorGCTargetDocumentAll:
		return w.safety.CanDeleteDocumentAll(ctx, task.TenantID, task.DocID)
	default:
		return false, fmt.Errorf("unsupported vector GC target kind %q", task.TargetKind)
	}
}

func (w *VectorGCWorker) delete(ctx context.Context, task *repository.VectorGCTask) error {
	switch task.TargetKind {
	case repository.VectorGCTargetGeneration:
		return w.vectors.DeleteByGeneration(ctx, task.TenantID, task.DocID, task.TargetGeneration)
	case repository.VectorGCTargetLegacy:
		return w.vectors.DeleteLegacyByDocID(ctx, task.TenantID, task.DocID)
	case repository.VectorGCTargetDocumentAll:
		return w.vectors.DeleteByDocID(ctx, task.TenantID, task.DocID)
	default:
		return fmt.Errorf("unsupported vector GC target kind %q", task.TargetKind)
	}
}

func (w *VectorGCWorker) retry(ctx context.Context, task *repository.VectorGCTask, token, cause string) {
	delay := vectorGCRetryDelay(task.AttemptCount)
	if ok, err := w.tasks.RetryIfOwned(ctx, task, token, time.Now().Add(delay), cause); err != nil {
		g.Log().Errorf(ctx, "VectorGCWorker: retry target=%s failed: %s", task.TargetKey, redact.Summary(err.Error(), 500))
	} else if ok {
		outcome := "retry_wait"
		if task.AttemptCount >= task.MaxAttempts {
			outcome = "dead"
		}
		observability.ObserveVectorGCAttempt(task.TargetKind, outcome)
	}
}

func vectorGCRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second
	for n := 1; n < attempt && delay < 5*time.Minute; n++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
