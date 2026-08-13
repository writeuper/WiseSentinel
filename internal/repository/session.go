package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
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
		Where("status", 1).
		Scan(&row)
	if err != nil {
		// A missing session is an ordinary authorization outcome. Normalise the
		// driver-specific no-row signal so the handler can return the stable 404
		// envelope rather than exposing it as a 500 diagnostic.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
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
	page, size = normalizePageBounds(page, size)
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
		Where("status", 1).
		Data(g.Map{"title": title}).Update()
	return err
}

// IsActive is a durable fence check used around Redis history writes. The
// database remains authoritative for deletion, rather than a process-local
// lock or a client-provided session state.
func (r *SessionRepo) IsActive(ctx context.Context, tenantID, sessionID string) (bool, error) {
	session, err := r.Get(ctx, tenantID, sessionID)
	if err != nil {
		return false, err
	}
	return session != nil, nil
}

func (r *SessionRepo) Touch(ctx context.Context, tenantID, sessionID string) error {
	_, err := g.DB().Model("ws_session").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("session_id", sessionID).
		Data(g.Map{"updated_at": time.Now()}).Update()
	return err
}

// Deactivate atomically hides a session from all normal reads. The Redis
// history is removed by the session service after this authorization fence is
// durable; a second delete is deliberately idempotent from the caller's view.
func (r *SessionRepo) Deactivate(ctx context.Context, tenantID, sessionID string) (bool, error) {
	result, err := g.DB().Model("ws_session").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("session_id", sessionID).
		Where("status", 1).
		Data(g.Map{"status": 0, "updated_at": time.Now()}).
		Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// DeactivateAndDeleteTurns forms the durable deletion boundary for a session
// and all of its synchronous-chat replay records. It is intentionally a DB
// transaction: deleting turn records before a failed session deactivation
// could otherwise make a live user turn re-execute, while deleting them later
// would retain data after the user-visible session is gone.
func (r *SessionRepo) DeactivateAndDeleteTurns(ctx context.Context, tenantID, sessionID string) (bool, error) {
	deactivated := false
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		result, err := tx.Model("ws_session").Ctx(ctx).
			Where("tenant_id", tenantID).
			Where("session_id", sessionID).
			Where("status", 1).
			Data(g.Map{"status": 0, "updated_at": time.Now()}).Update()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		deactivated = true
		_, err = tx.Model("ws_chat_turn").Ctx(ctx).
			Where("tenant_id", tenantID).
			Where("session_id", sessionID).
			Delete()
		return err
	})
	return deactivated, err
}
