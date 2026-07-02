package repository

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Session is a ws_session row.
type Session struct {
	TenantID  string
	SessionID string
	UserID    string
	Title     string
	AgentType string
	Status    int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionRepo manages ws_session persistence.
type SessionRepo struct{}

func NewSessionRepo() *SessionRepo {
	return &SessionRepo{}
}

func (r *SessionRepo) Create(ctx context.Context, s *Session) error {
	_, err := g.DB().Insert(ctx, "ws_session", g.Map{
		"tenant_id":  s.TenantID,
		"session_id": s.SessionID,
		"user_id":    s.UserID,
		"title":      s.Title,
		"agent_type": s.AgentType,
		"status":     1,
	})
	return err
}

func (r *SessionRepo) Get(ctx context.Context, tenantID, sessionID string) (*Session, error) {
	var row struct {
		TenantID  string    `json:"tenant_id"`
		SessionID string    `json:"session_id"`
		UserID    string    `json:"user_id"`
		Title     string    `json:"title"`
		AgentType string    `json:"agent_type"`
		Status    int       `json:"status"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	err := g.DB().Model("ws_session").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("session_id", sessionID).
		Scan(&row)
	if err != nil {
		return nil, err
	}
	if row.SessionID == "" {
		return nil, nil
	}
	return &Session{
		TenantID:  row.TenantID,
		SessionID: row.SessionID,
		UserID:    row.UserID,
		Title:     row.Title,
		AgentType: row.AgentType,
		Status:    row.Status,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *SessionRepo) ListByUser(ctx context.Context, tenantID, userID string, page, size int) ([]Session, int, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	model := g.DB().Model("ws_session").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("user_id", userID).
		Where("status", 1)

	total, err := model.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []struct {
		TenantID  string    `json:"tenant_id"`
		SessionID string    `json:"session_id"`
		UserID    string    `json:"user_id"`
		Title     string    `json:"title"`
		AgentType string    `json:"agent_type"`
		Status    int       `json:"status"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	err = model.Page(page, size).OrderDesc("updated_at").Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	items := make([]Session, len(rows))
	for i, row := range rows {
		items[i] = Session{
			TenantID:  row.TenantID,
			SessionID: row.SessionID,
			UserID:    row.UserID,
			Title:     row.Title,
			AgentType: row.AgentType,
			Status:    row.Status,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}
	}
	return items, total, nil
}

func (r *SessionRepo) UpdateTitle(ctx context.Context, tenantID, sessionID, title string) error {
	_, err := g.DB().Model("ws_session").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("session_id", sessionID).
		Data(g.Map{"title": title}).Update()
	return err
}

func (r *SessionRepo) Touch(ctx context.Context, tenantID, sessionID string) error {
	_, err := g.DB().Model("ws_session").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("session_id", sessionID).
		Data(g.Map{"updated_at": time.Now()}).Update()
	return err
}