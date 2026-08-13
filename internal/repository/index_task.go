package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// IndexTaskRecord is a ws_index_task row.
type IndexTaskRecord struct {
	TenantID       string
	TaskID         string
	DocID          string
	SourceURI      string
	Visibility     string
	SecretLevel    int
	Layer          domain.KnowledgeLayer
	Version        string
	Service        string
	Status         string
	ChunkCount     int
	ErrorMsg       string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	ExecutionToken string
	LeaseExpiresAt *time.Time
	AttemptCount   int
	MaxAttempts    int
	NextAttemptAt  *time.Time
	Generation     uint64
	CreatedAt      time.Time
}

// IndexTaskRepo manages ws_index_task persistence.
type IndexTaskRepo struct{}

func NewIndexTaskRepo() *IndexTaskRepo {
	return &IndexTaskRepo{}
}

func (r *IndexTaskRepo) Create(ctx context.Context, task *IndexTaskRecord) error {
	_, err := g.DB().Insert(ctx, "ws_index_task", g.Map{
		"tenant_id":    task.TenantID,
		"task_id":      task.TaskID,
		"doc_id":       task.DocID,
		"source_uri":   task.SourceURI,
		"visibility":   task.Visibility,
		"secret_level": task.SecretLevel,
		"layer":        string(task.Layer),
		"version":      task.Version,
		"service":      task.Service,
		"status":       task.Status,
		"max_attempts": func() int {
			if task.MaxAttempts > 0 {
				return task.MaxAttempts
			}
			return 3
		}(),
		"generation": task.Generation,
	})
	return err
}

func (r *IndexTaskRepo) ListRunnable(ctx context.Context, limit int, now time.Time) ([]*IndexTaskRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	var tasks []*IndexTaskRecord
	err := g.DB().Ctx(ctx).Model("ws_index_task").
		Where("status = ? OR (status = ? AND (lease_expires_at IS NULL OR lease_expires_at < ?)) OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))",
			string(domain.IndexTaskPending), string(domain.IndexTaskRunning), now, string(domain.IndexTaskRetryWait), now).
		Order("created_at ASC").
		Limit(limit).
		Scan(&tasks)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *IndexTaskRepo) ClaimRunnable(ctx context.Context, tenantID, taskID, executionToken string, leaseUntil time.Time) (bool, error) {
	now := time.Now()
	result, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status = ? OR (status = ? AND (lease_expires_at IS NULL OR lease_expires_at < ?)) OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))",
			string(domain.IndexTaskPending), string(domain.IndexTaskRunning), now, string(domain.IndexTaskRetryWait), now).
		Data(g.Map{
			"status":           string(domain.IndexTaskRunning),
			"started_at":       now,
			"finished_at":      nil,
			"execution_token":  executionToken,
			"lease_expires_at": leaseUntil,
			"error_msg":        "",
			"next_attempt_at":  nil,
			"attempt_count":    gdb.Raw("attempt_count + 1"),
		}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// RenewLeaseIfOwned extends a still-valid database execution lease. The token
// check prevents a stale worker from reviving a task reclaimed by another.
func (r *IndexTaskRepo) RenewLeaseIfOwned(ctx context.Context, tenantID, taskID, executionToken string, leaseUntil time.Time) (bool, error) {
	result, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).Where("task_id", taskID).
		Where("status", string(domain.IndexTaskRunning)).Where("execution_token", executionToken).
		Where("lease_expires_at > ?", time.Now()).
		Data(g.Map{"lease_expires_at": leaseUntil}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (r *IndexTaskRepo) CancelActive(ctx context.Context, tenantID, docID string) error {
	_, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("doc_id", docID).
		WhereIn("status", []string{string(domain.IndexTaskPending), string(domain.IndexTaskRunning)}).
		Data(g.Map{
			"status":           string(domain.IndexTaskFailed),
			"error_msg":        "document deleted",
			"finished_at":      time.Now(),
			"execution_token":  "",
			"lease_expires_at": nil,
			"next_attempt_at":  nil,
		}).Update()
	return err
}

func (r *IndexTaskRepo) MarkFinished(ctx context.Context, tenantID, taskID, status string, chunkCount int, errMsg string) error {
	_, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":           status,
			"chunk_count":      chunkCount,
			"error_msg":        redact.Summary(errMsg, 2000),
			"finished_at":      time.Now(),
			"lease_expires_at": nil,
		}).Update()
	return err
}

func (r *IndexTaskRepo) MarkFinishedIfRunning(ctx context.Context, tenantID, taskID, status string, chunkCount int, errMsg string) (bool, error) {
	result, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status", string(domain.IndexTaskRunning)).
		Where("lease_expires_at > ?", time.Now()).
		Data(g.Map{
			"status":           status,
			"chunk_count":      chunkCount,
			"error_msg":        errMsg,
			"finished_at":      time.Now(),
			"lease_expires_at": nil,
		}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// MarkFinishedIfOwned publishes a result only if this worker still owns the
// running attempt. A stale worker must never overwrite a reclaimed task.
func (r *IndexTaskRepo) MarkFinishedIfOwned(ctx context.Context, tenantID, taskID, executionToken, status string, chunkCount int, errMsg string) (bool, error) {
	result, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status", string(domain.IndexTaskRunning)).
		Where("execution_token", executionToken).
		Where("lease_expires_at > ?", time.Now()).
		Data(g.Map{
			"status":           status,
			"chunk_count":      chunkCount,
			"error_msg":        redact.Summary(errMsg, 2000),
			"finished_at":      time.Now(),
			"execution_token":  "",
			"lease_expires_at": nil,
		}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// RetryIfOwned releases a transient index failure for a bounded durable retry.
func (r *IndexTaskRepo) RetryIfOwned(ctx context.Context, task *IndexTaskRecord, token string, retryAt time.Time, errMsg string) (bool, error) {
	if task == nil {
		return false, errors.New("index task is required")
	}
	status := string(domain.IndexTaskRetryWait)
	finishedAt := any(nil)
	if task.AttemptCount >= task.MaxAttempts {
		status = string(domain.IndexTaskFailed)
		finishedAt = time.Now()
		retryAt = time.Time{}
	}
	data := g.Map{"status": status, "execution_token": "", "lease_expires_at": nil, "error_msg": redact.Summary(errMsg, 2000), "finished_at": finishedAt}
	if status == string(domain.IndexTaskRetryWait) {
		data["next_attempt_at"] = retryAt
	} else {
		data["next_attempt_at"] = nil
	}
	result, err := g.DB().Ctx(ctx).Model("ws_index_task").Where("tenant_id", task.TenantID).Where("task_id", task.TaskID).Where("status", string(domain.IndexTaskRunning)).Where("execution_token", token).Data(data).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (r *IndexTaskRepo) Get(ctx context.Context, tenantID, taskID string) (*IndexTaskRecord, error) {
	var row struct {
		TenantID       string     `json:"tenant_id"`
		TaskID         string     `json:"task_id"`
		DocID          string     `json:"doc_id"`
		SourceURI      string     `json:"source_uri"`
		Visibility     string     `json:"visibility"`
		SecretLevel    int        `json:"secret_level"`
		Layer          string     `json:"layer"`
		Version        string     `json:"version"`
		Service        string     `json:"service"`
		Status         string     `json:"status"`
		ChunkCount     int        `json:"chunk_count"`
		ErrorMsg       string     `json:"error_msg"`
		StartedAt      *time.Time `json:"started_at"`
		FinishedAt     *time.Time `json:"finished_at"`
		ExecutionToken string     `json:"execution_token"`
		LeaseExpiresAt *time.Time `json:"lease_expires_at"`
		AttemptCount   int        `json:"attempt_count"`
		MaxAttempts    int        `json:"max_attempts"`
		NextAttemptAt  *time.Time `json:"next_attempt_at"`
		Generation     uint64     `json:"generation"`
		CreatedAt      time.Time  `json:"created_at"`
	}
	err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Scan(&row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.TaskID == "" {
		return nil, nil
	}
	return &IndexTaskRecord{
		TenantID:       row.TenantID,
		TaskID:         row.TaskID,
		DocID:          row.DocID,
		SourceURI:      row.SourceURI,
		Visibility:     row.Visibility,
		SecretLevel:    row.SecretLevel,
		Layer:          domain.KnowledgeLayer(row.Layer),
		Version:        row.Version,
		Service:        row.Service,
		Status:         row.Status,
		ChunkCount:     row.ChunkCount,
		ErrorMsg:       row.ErrorMsg,
		StartedAt:      row.StartedAt,
		FinishedAt:     row.FinishedAt,
		ExecutionToken: row.ExecutionToken,
		LeaseExpiresAt: row.LeaseExpiresAt,
		AttemptCount:   row.AttemptCount, MaxAttempts: row.MaxAttempts, NextAttemptAt: row.NextAttemptAt,
		Generation: row.Generation,
		CreatedAt:  row.CreatedAt,
	}, nil
}
