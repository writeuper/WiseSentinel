package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	ChatTurnRunning   = "running"
	ChatTurnSucceeded = "succeeded"
	ChatTurnFailed    = "failed"
)

// ChatTurn is the durable idempotency record for one synchronous user turn.
// It deliberately contains request and response hashes/projections only; the
// raw Idempotency-Key never appears in tracing or logs.
type ChatTurn struct {
	TenantID       string
	UserID         string
	SessionID      string
	IdempotencyKey string
	RequestHash    string
	Status         string
	ExecutionToken string
	ResponseJSON   string
	TraceID        string
	ErrorClass     string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

// ChatTurnRepo owns the MySQL uniqueness/CAS boundary for synchronous chat
// retries. Redis locks are intentionally not used for correctness.
type ChatTurnRepo struct{}

func NewChatTurnRepo() *ChatTurnRepo { return &ChatTurnRepo{} }

// Claim atomically creates one running turn or returns the existing record.
// The caller that receives created=true owns execution and must use its token
// to make a terminal transition.
func (r *ChatTurnRepo) Claim(ctx context.Context, turn *ChatTurn) (existing *ChatTurn, created bool, err error) {
	if turn == nil {
		return nil, false, errors.New("chat turn is required")
	}
	result, err := g.DB().InsertIgnore(ctx, "ws_chat_turn", g.Map{
		"tenant_id":       turn.TenantID,
		"user_id":         turn.UserID,
		"session_id":      turn.SessionID,
		"idempotency_key": turn.IdempotencyKey,
		"request_hash":    turn.RequestHash,
		"status":          ChatTurnRunning,
		"execution_token": turn.ExecutionToken,
	})
	if err != nil {
		return nil, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if rows > 0 {
		turn.Status = ChatTurnRunning
		return turn, true, nil
	}
	stored, err := r.Get(ctx, turn.TenantID, turn.UserID, turn.SessionID, turn.IdempotencyKey)
	return stored, false, err
}

func (r *ChatTurnRepo) Get(ctx context.Context, tenantID, userID, sessionID, key string) (*ChatTurn, error) {
	var row ChatTurn
	err := g.DB().Model("ws_chat_turn").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("user_id", userID).
		Where("session_id", sessionID).
		Where("idempotency_key", key).
		Scan(&row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.IdempotencyKey == "" {
		return nil, nil
	}
	return &row, nil
}

func (r *ChatTurnRepo) Succeed(ctx context.Context, turn *ChatTurn, responseJSON, traceID string) (bool, error) {
	result, err := g.DB().Model("ws_chat_turn").Ctx(ctx).
		Where("tenant_id", turn.TenantID).
		Where("user_id", turn.UserID).
		Where("session_id", turn.SessionID).
		Where("idempotency_key", turn.IdempotencyKey).
		Where("status", ChatTurnRunning).
		Where("execution_token", turn.ExecutionToken).
		Data(g.Map{"status": ChatTurnSucceeded, "response_json": responseJSON, "trace_id": traceID, "finished_at": time.Now()}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (r *ChatTurnRepo) Fail(ctx context.Context, turn *ChatTurn, errorClass string) error {
	_, err := g.DB().Model("ws_chat_turn").Ctx(ctx).
		Where("tenant_id", turn.TenantID).
		Where("user_id", turn.UserID).
		Where("session_id", turn.SessionID).
		Where("idempotency_key", turn.IdempotencyKey).
		Where("status", ChatTurnRunning).
		Where("execution_token", turn.ExecutionToken).
		Data(g.Map{"status": ChatTurnFailed, "error_class": errorClass, "finished_at": time.Now()}).Update()
	return err
}
