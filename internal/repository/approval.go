package repository

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Approval is a ws_approval row.
type Approval struct {
	TenantID     string
	ApprovalID   string
	TaskID       string
	ApprovalType string
	PayloadJSON  string
	Status       string
	ApproverID   string
	Comment      string
	ExpiredAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ApprovalRepo manages ws_approval persistence.
type ApprovalRepo struct{}

// NewApprovalRepo creates an approval repo.
func NewApprovalRepo() *ApprovalRepo { return &ApprovalRepo{} }

// Create inserts a new approval record.
func (r *ApprovalRepo) Create(ctx context.Context, a *Approval) error {
	_, err := g.DB().Insert(ctx, "ws_approval", g.Map{
		"tenant_id":     a.TenantID,
		"approval_id":   a.ApprovalID,
		"task_id":       a.TaskID,
		"approval_type": a.ApprovalType,
		"payload_json":  a.PayloadJSON,
		"status":        a.Status,
		"expired_at":    a.ExpiredAt,
	})
	return err
}

// GetPending returns a pending approval by ID.
func (r *ApprovalRepo) GetPending(ctx context.Context, tenantID, approvalID string) (*Approval, error) {
	var row Approval
	err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("approval_id", approvalID).
		Where("status", "pending").
		Scan(&row)
	if err != nil {
		return nil, err
	}
	if row.ApprovalID == "" {
		return nil, nil
	}
	return &row, nil
}

// ListPending returns pending approvals for a tenant, paginated.
func (r *ApprovalRepo) ListPending(ctx context.Context, tenantID string, page, size int) ([]*Approval, int, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	var rows []*Approval
	err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("status", "pending").
		OrderAsc("created_at").
		Page(page, size).
		Scan(&rows)
	if err != nil {
		return nil, 0, err
	}
	total, err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("status", "pending").
		Count()
	if err != nil {
		return rows, 0, nil
	}
	return rows, total, nil
}

// Decide updates an approval record with the decision.
func (r *ApprovalRepo) Decide(ctx context.Context, tenantID, approvalID, decision, approverID, comment string) (bool, error) {
	now := time.Now()
	result, err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("approval_id", approvalID).
		Where("status", "pending").
		Data(g.Map{
			"status":      decision,
			"approver_id": approverID,
			"comment":     comment,
			"updated_at":  now,
		}).
		Update()
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

// ExpireStale marks all expired pending approvals as expired.
func (r *ApprovalRepo) ExpireStale(ctx context.Context) error {
	_, err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("status", "pending").
		Where("expired_at <", time.Now()).
		Data(g.Map{"status": "expired"}).
		Update()
	return err
}
