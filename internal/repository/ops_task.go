package repository

import (
	"context"
	"time"

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
		CreatedAt:   row.CreatedAt,
	}, nil
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