package repository

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// IndexTaskRecord is a ws_index_task row.
type IndexTaskRecord struct {
	TenantID   string
	TaskID     string
	DocID      string
	Status     string
	ChunkCount int
	ErrorMsg   string
	StartedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
}

// IndexTaskRepo manages ws_index_task persistence.
type IndexTaskRepo struct{}

func NewIndexTaskRepo() *IndexTaskRepo {
	return &IndexTaskRepo{}
}

func (r *IndexTaskRepo) Create(ctx context.Context, task *IndexTaskRecord) error {
	_, err := g.DB().Insert(ctx, "ws_index_task", g.Map{
		"tenant_id": task.TenantID,
		"task_id":   task.TaskID,
		"doc_id":    task.DocID,
		"status":    task.Status,
	})
	return err
}

func (r *IndexTaskRepo) MarkRunning(ctx context.Context, tenantID, taskID string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":     "running",
			"started_at": now,
		}).Update()
	return err
}

func (r *IndexTaskRepo) MarkFinished(ctx context.Context, tenantID, taskID, status string, chunkCount int, errMsg string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Data(g.Map{
			"status":      status,
			"chunk_count": chunkCount,
			"error_msg":   errMsg,
			"finished_at": now,
		}).Update()
	return err
}

func (r *IndexTaskRepo) Get(ctx context.Context, tenantID, taskID string) (*IndexTaskRecord, error) {
	var row struct {
		TenantID   string     `json:"tenant_id"`
		TaskID     string     `json:"task_id"`
		DocID      string     `json:"doc_id"`
		Status     string     `json:"status"`
		ChunkCount int        `json:"chunk_count"`
		ErrorMsg   string     `json:"error_msg"`
		StartedAt  *time.Time `json:"started_at"`
		FinishedAt *time.Time `json:"finished_at"`
		CreatedAt  time.Time  `json:"created_at"`
	}
	err := g.DB().Model("ws_index_task").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("task_id", taskID).
		Scan(&row)
	if err != nil {
		return nil, err
	}
	if row.TaskID == "" {
		return nil, nil
	}
	return &IndexTaskRecord{
		TenantID:   row.TenantID,
		TaskID:     row.TaskID,
		DocID:      row.DocID,
		Status:     row.Status,
		ChunkCount: row.ChunkCount,
		ErrorMsg:   row.ErrorMsg,
		StartedAt:  row.StartedAt,
		FinishedAt: row.FinishedAt,
		CreatedAt:  row.CreatedAt,
	}, nil
}
