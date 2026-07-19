package repository

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// OpsTask is a ws_ops_task row.
type OpsTask struct {
	TenantID    string
	TaskID      string
	TriggerType string
	InputQuery  string
	Result      string
	DetailJSON  string
	Status      string
	TraceID     string
	CreatedBy   string
	StartedAt   *time.Time
	FinishedAt  *time.Time
	RetryCount  int
	MaxRetry    int
	TimeoutAt   *time.Time
	LastError   string
	CreatedAt   time.Time
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
		TenantID    string     `json:"tenant_id"`
		TaskID      string     `json:"task_id"`
		TriggerType string     `json:"trigger_type"`
		InputQuery  string     `json:"input_query"`
		Result      string     `json:"result"`
		DetailJSON  string     `json:"detail_json"`
		Status      string     `json:"status"`
		TraceID     string     `json:"trace_id"`
		CreatedBy   string     `json:"created_by"`
		StartedAt   *time.Time `json:"started_at"`
		FinishedAt  *time.Time `json:"finished_at"`
		RetryCount  int        `json:"retry_count"`
		MaxRetry    int        `json:"max_retry"`
		TimeoutAt   *time.Time `json:"timeout_at"`
		LastError   string     `json:"last_error"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Scan(&row)
	if err != nil {
		return nil, err
	}
	if row.TaskID == "" {
		return nil, nil
	}
	return &OpsTask{
		TenantID:    row.TenantID,
		TaskID:      row.TaskID,
		TriggerType: row.TriggerType,
		InputQuery:  row.InputQuery,
		Result:      row.Result,
		DetailJSON:  row.DetailJSON,
		Status:      row.Status,
		TraceID:     row.TraceID,
		CreatedBy:   row.CreatedBy,
		StartedAt:   row.StartedAt,
		FinishedAt:  row.FinishedAt,
		RetryCount:  row.RetryCount,
		MaxRetry:    row.MaxRetry,
		TimeoutAt:   row.TimeoutAt,
		LastError:   row.LastError,
		CreatedAt:   row.CreatedAt,
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
	err := g.DB().Ctx(ctx).Model("ws_ops_task").
		WhereIn("status", []string{"pending", "retrying"}).
		Order("created_at ASC").
		Limit(limit).
		Scan(&tasks)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// ListByTenant returns the page-indexed list of ops tasks for a tenant,
// optionally filtered by status. Most recent first.
func (r *OpsTaskRepo) ListByTenant(ctx context.Context, tenantID, status string, page, size int) ([]*OpsTask, int, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
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
		return tasks, 0, nil
	}
	return tasks, total, nil
}

func (r *OpsTaskRepo) MarkRunning(ctx context.Context, tenantID, taskID string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{"status": "running", "started_at": now}).Update()
	return err
}

func (r *OpsTaskRepo) MarkFinished(ctx context.Context, tenantID, taskID, status, result, detailJSON string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":      status,
			"result":      result,
			"detail_json": detailJSON,
			"finished_at": now,
		}).Update()
	return err
}

func (r *OpsTaskRepo) Retry(ctx context.Context, tenantID, taskID, errMsg string) error {
	_, err := g.DB().Model("ws_ops_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":      "retrying",
			"retry_count": gdb.Raw("retry_count + 1"),
			"last_error":  errMsg,
		}).Update()
	return err
}
