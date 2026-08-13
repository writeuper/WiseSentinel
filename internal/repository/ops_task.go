package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"wisesentinel-platform/internal/gateway/metrics"
	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// OpsTask is a ws_ops_task row.
type OpsTask struct {
	TenantID       string
	TaskID         string
	TriggerType    string
	InputQuery     string
	Result         string
	DetailJSON     string
	Status         string
	TraceID        string
	CreatedBy      string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	RetryCount     int
	MaxRetry       int
	TimeoutAt      *time.Time
	NextAttemptAt  *time.Time
	ExecutionToken string
	LastError      string
	CreatedAt      time.Time
}

// OpsTaskRepo manages ws_ops_task persistence.
type OpsTaskRepo struct{}

func NewOpsTaskRepo() *OpsTaskRepo {
	return &OpsTaskRepo{}
}

func (r *OpsTaskRepo) Create(ctx context.Context, task *OpsTask) error {
	_, err := g.DB().Insert(ctx, "ws_ops_task", g.Map{
		"tenant_id":    task.TenantID,
		"task_id":      task.TaskID,
		"trigger_type": task.TriggerType,
		"input_query":  task.InputQuery,
		"status":       task.Status,
		"trace_id":     task.TraceID,
		"created_by":   task.CreatedBy,
		"max_retry":    task.MaxRetry,
	})
	return err
}

func (r *OpsTaskRepo) Get(ctx context.Context, tenantID, taskID string) (*OpsTask, error) {
	var row struct {
		TenantID       string     `json:"tenant_id"`
		TaskID         string     `json:"task_id"`
		TriggerType    string     `json:"trigger_type"`
		InputQuery     string     `json:"input_query"`
		Result         string     `json:"result"`
		DetailJSON     string     `json:"detail_json"`
		Status         string     `json:"status"`
		TraceID        string     `json:"trace_id"`
		CreatedBy      string     `json:"created_by"`
		StartedAt      *time.Time `json:"started_at"`
		FinishedAt     *time.Time `json:"finished_at"`
		RetryCount     int        `json:"retry_count"`
		MaxRetry       int        `json:"max_retry"`
		TimeoutAt      *time.Time `json:"timeout_at"`
		NextAttemptAt  *time.Time `json:"next_attempt_at"`
		ExecutionToken string     `json:"execution_token"`
		LastError      string     `json:"last_error"`
		CreatedAt      time.Time  `json:"created_at"`
	}
	err := g.DB().Model("ws_ops_task").Ctx(ctx).
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
	return &OpsTask{
		TenantID:       row.TenantID,
		TaskID:         row.TaskID,
		TriggerType:    row.TriggerType,
		InputQuery:     row.InputQuery,
		Result:         row.Result,
		DetailJSON:     row.DetailJSON,
		Status:         row.Status,
		TraceID:        row.TraceID,
		CreatedBy:      row.CreatedBy,
		StartedAt:      row.StartedAt,
		FinishedAt:     row.FinishedAt,
		RetryCount:     row.RetryCount,
		MaxRetry:       row.MaxRetry,
		TimeoutAt:      row.TimeoutAt,
		NextAttemptAt:  row.NextAttemptAt,
		ExecutionToken: row.ExecutionToken,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
	}, nil
}

// ListPending returns the most recent N pending tasks for any tenant.
// The worker processes all pending tasks across tenants in FIFO order
// (filtered and locked per-task).
func (r *OpsTaskRepo) ListPending(ctx context.Context, limit int) ([]*OpsTask, error) {
	if limit <= 0 {
		limit = 10
	}
	var tasks []*OpsTask
	now := time.Now()
	err := g.DB().Ctx(ctx).Model("ws_ops_task").
		Where("status = ? OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))", "pending", "retrying", now).
		Order("created_at ASC").
		Limit(limit).
		Scan(&tasks)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// ClaimRunnable atomically grants one execution token ownership of a runnable
// task. MySQL is the ownership authority; Redis locks only reduce contention.
func (r *OpsTaskRepo) ClaimRunnable(ctx context.Context, tenantID, taskID, executionToken string, timeoutAt time.Time) (bool, error) {
	now := time.Now()
	result, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status = ? OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))", "pending", "retrying", now).
		Data(g.Map{
			"status":          "running",
			"started_at":      now,
			"finished_at":     nil,
			"timeout_at":      timeoutAt,
			"next_attempt_at": nil,
			"execution_token": executionToken,
			"last_error":      "",
		}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// ListByTenant returns the page-indexed list of ops tasks for a tenant,
// optionally filtered by status. Most recent first.
func (r *OpsTaskRepo) ListByTenant(ctx context.Context, tenantID, status string, page, size int) ([]*OpsTask, int, error) {
	page, size = normalizePageBounds(page, size)
	model := g.DB().Ctx(ctx).Model("ws_ops_task").
		Where("tenant_id", tenantID)
	if status != "" {
		model = model.Where("status", status)
	}
	var tasks []*OpsTask
	if err := model.OrderDesc("created_at").Page(page, size).Scan(&tasks); err != nil {
		return nil, 0, err
	}
	total, err := g.DB().Ctx(ctx).Model("ws_ops_task").
		Where("tenant_id", tenantID).
		Count()
	if err != nil {
		return tasks, 0, err
	}
	return tasks, total, nil
}

// ListByTenantAndCreator returns only tasks owned by a user. It is used for
// object-level authorization; callers with administrative roles use
// ListByTenant instead.
func (r *OpsTaskRepo) ListByTenantAndCreator(ctx context.Context, tenantID, createdBy, status string, page, size int) ([]*OpsTask, int, error) {
	page, size = normalizePageBounds(page, size)
	model := g.DB().Ctx(ctx).Model("ws_ops_task").
		Where("tenant_id", tenantID).
		Where("created_by", createdBy)
	if status != "" {
		model = model.Where("status", status)
	}
	var tasks []*OpsTask
	if err := model.OrderDesc("created_at").Page(page, size).Scan(&tasks); err != nil {
		return nil, 0, err
	}
	total, err := model.Count()
	if err != nil {
		return tasks, 0, err
	}
	return tasks, total, nil
}

func (r *OpsTaskRepo) MarkRunning(ctx context.Context, tenantID, taskID string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":     "running",
			"started_at": now,
			"last_error": "",
		}).Update()
	return err
}

// FinishIfOwned records a terminal result only when the caller still owns the
// running attempt. A zero affected-row count means the lease was superseded.
func (r *OpsTaskRepo) FinishIfOwned(ctx context.Context, tenantID, taskID, executionToken, status, resultText, detailJSON string) (bool, error) {
	now := time.Now()
	// ws_ops_task is an operations projection, not an evidence vault. Keep the
	// task state and payload sizes available without retaining model/tool bodies.
	resultProjection := redact.TelemetryProjection(resultText)
	detailProjection := redact.TelemetryProjection(detailJSON)
	data := g.Map{
		"status":          status,
		"result":          resultProjection,
		"detail_json":     detailProjection,
		"finished_at":     now,
		"execution_token": "",
	}
	if status == "failed" || status == "timeout" {
		data["last_error"] = resultProjection
	} else {
		data["last_error"] = ""
	}
	result, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status", "running").
		Where("execution_token", executionToken).
		Data(data).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if rows > 0 {
		if task, getErr := r.Get(ctx, tenantID, taskID); getErr == nil && task != nil {
			recordOpsTaskMetrics(task, status, now)
		}
	}
	return rows > 0, err
}

func (r *OpsTaskRepo) MarkFinished(ctx context.Context, tenantID, taskID, status, result, detailJSON string) error {
	now := time.Now()
	resultProjection := redact.TelemetryProjection(result)
	detailProjection := redact.TelemetryProjection(detailJSON)
	if task, err := r.Get(ctx, tenantID, taskID); err == nil && task != nil {
		recordOpsTaskMetrics(task, status, now)
	}
	data := g.Map{
		"status":      status,
		"result":      resultProjection,
		"detail_json": detailProjection,
		"finished_at": now,
	}
	if status == "failed" || status == "timeout" {
		data["last_error"] = resultProjection
	} else {
		data["last_error"] = ""
	}
	_, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(data).Update()
	return err
}

func recordOpsTaskMetrics(task *OpsTask, status string, finishedAt time.Time) {
	if task == nil {
		return
	}
	if task.StartedAt != nil && !task.CreatedAt.IsZero() {
		metrics.ObserveOpsTaskDuration("queue", status, task.StartedAt.Sub(task.CreatedAt).Seconds())
	}
	if task.StartedAt != nil {
		metrics.ObserveOpsTaskDuration("run", status, finishedAt.Sub(*task.StartedAt).Seconds())
	}
	if !task.CreatedAt.IsZero() {
		metrics.ObserveOpsTaskDuration("e2e", status, finishedAt.Sub(task.CreatedAt).Seconds())
	}
}

func (r *OpsTaskRepo) Retry(ctx context.Context, tenantID, taskID, errMsg string) error {
	_, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":      "retrying",
			"retry_count": gdb.Raw("retry_count + 1"),
			"last_error":  redact.TelemetryProjection(errMsg),
		}).Update()
	return err
}

// RetryOrFailIfOwned schedules a bounded retry or transitions the owned
// attempt to failed once its retry budget is exhausted.
func (r *OpsTaskRepo) RetryOrFailIfOwned(ctx context.Context, tenantID, taskID, executionToken, errMsg string, nextAttemptAt time.Time) (retried, changed bool, err error) {
	errProjection := redact.TelemetryProjection(errMsg)
	retryResult, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status", "running").
		Where("execution_token", executionToken).
		Where("retry_count < max_retry").
		Data(g.Map{
			"status":          "retrying",
			"retry_count":     gdb.Raw("retry_count + 1"),
			"next_attempt_at": nextAttemptAt,
			"execution_token": "",
			"last_error":      errProjection,
		}).Update()
	if err != nil {
		return false, false, err
	}
	rows, err := retryResult.RowsAffected()
	if err != nil {
		return false, false, err
	}
	if rows > 0 {
		return true, true, nil
	}

	now := time.Now()
	failResult, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Where("status", "running").
		Where("execution_token", executionToken).
		Data(g.Map{
			"status":          "failed",
			"result":          errProjection,
			"finished_at":     now,
			"execution_token": "",
			"last_error":      errProjection,
		}).Update()
	if err != nil {
		return false, false, err
	}
	rows, err = failResult.RowsAffected()
	if rows > 0 {
		if task, getErr := r.Get(ctx, tenantID, taskID); getErr == nil && task != nil {
			recordOpsTaskMetrics(task, "failed", now)
		}
	}
	return false, rows > 0, err
}

// ExpireTimedOut fences stale running attempts. Late owners cannot overwrite
// the terminal timeout because FinishIfOwned requires status=running.
func (r *OpsTaskRepo) ExpireTimedOut(ctx context.Context, now time.Time) (int64, error) {
	result, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("status", "running").
		Where("timeout_at IS NOT NULL AND timeout_at <= ?", now).
		Data(g.Map{
			"status":          "timeout",
			"result":          "ops task execution timed out",
			"finished_at":     now,
			"execution_token": "",
			"last_error":      "ops task execution timed out",
		}).Update()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RetryBackoff returns a bounded exponential retry delay. retryCount is the
// number of retries already persisted before scheduling the next one.
func RetryBackoff(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	delay := 5 * time.Second * time.Duration(1<<min(retryCount, 6))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
