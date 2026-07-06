package repository

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
)

// UserRoleRepo manages ws_user_role.
type UserRoleRepo struct{}

// NewUserRoleRepo creates a user role repo.
func NewUserRoleRepo() *UserRoleRepo { return &UserRoleRepo{} }

// ListByUser returns all role strings for (tenant_id, user_id).
func (r *UserRoleRepo) ListByUser(ctx context.Context, tenantID, userID string) ([]string, error) {
	type row struct {
		Role string
	}
	var rows []row
	err := g.DB().Model("ws_user_role").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("user_id", userID).
		Fields("role").
		Scan(&rows)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Role)
	}
	return out, nil
}
