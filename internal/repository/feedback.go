package repository

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
)

type Feedback struct {
	TenantID   string
	FeedbackID string
	TargetType string
	TargetID   string
	Rating     string
	Comment    string
	CreatedBy  string
}

type FeedbackRepo struct{}

func NewFeedbackRepo() *FeedbackRepo { return &FeedbackRepo{} }

func (r *FeedbackRepo) Create(ctx context.Context, fb *Feedback) error {
	_, err := g.DB().Insert(ctx, "ws_feedback", g.Map{
		"tenant_id":   fb.TenantID,
		"feedback_id": fb.FeedbackID,
		"target_type": fb.TargetType,
		"target_id":   fb.TargetID,
		"rating":      fb.Rating,
		"comment":     fb.Comment,
		"created_by":  fb.CreatedBy,
	})
	return err
}
