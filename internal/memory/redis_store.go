// Package memory implements SessionService backed by Redis + MySQL.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

const (
	maxWindowSize = 6 // 3 rounds (user + assistant)
	ttlSeconds    = 7 * 86400
)

// RedisSessionStore implements domain.SessionService with Redis + MySQL.
type RedisSessionStore struct {
	sessions *repository.SessionRepo
}

// NewRedisSessionStore creates a session store.
func NewRedisSessionStore(sessionRepo *repository.SessionRepo) *RedisSessionStore {
	return &RedisSessionStore{sessions: sessionRepo}
}

// CreateSession creates a new session in MySQL and returns the session ID.
func (s *RedisSessionStore) CreateSession(ctx context.Context, tenantID, userID string, opts ...domain.SessionOption) (string, error) {
	cfg := domain.DefaultSessionConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	sessionID := "sess_" + uuid.NewString()
	title := cfg.Title
	if title == "" {
		title = "新对话"
	}
	agentType := cfg.AgentType
	if agentType == "" {
		agentType = string(domain.AgentTypeChat)
	}
	record := &repository.Session{
		TenantID:  tenantID,
		SessionID: sessionID,
		UserID:    userID,
		Title:     title,
		AgentType: agentType,
	}
	if err := s.sessions.Create(ctx, record); err != nil {
		return "", apperr.Wrap(err, apperr.ErrInternal)
	}
	return sessionID, nil
}

// GetHistory reads messages from Redis List.
func (s *RedisSessionStore) GetHistory(ctx context.Context, tenantID, sessionID string) ([]*domain.Message, error) {
	key := redisKey(tenantID, sessionID)
	vals, err := g.Redis().Do(ctx, "LRANGE", key, 0, -1)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	items := vals.Interfaces()
	if len(items) == 0 {
		return nil, nil
	}
	msgs := make([]*domain.Message, 0, len(items))
	for _, item := range items {
		str, ok := item.(string)
		if !ok {
			continue
		}
		var msg domain.Message
		if err := json.Unmarshal([]byte(str), &msg); err != nil {
			continue
		}
		msgs = append(msgs, &msg)
	}
	return msgs, nil
}

// AppendMessages serializes and RPUSHes messages, then LTRIM and EXPIRE.
func (s *RedisSessionStore) AppendMessages(ctx context.Context, tenantID, sessionID string, msgs ...*domain.Message) error {
	key := redisKey(tenantID, sessionID)
	redis := g.Redis()

	for _, msg := range msgs {
		if msg.Timestamp == "" {
			msg.Timestamp = time.Now().UTC().Format(time.RFC3339)
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return apperr.Wrap(err, apperr.ErrInternal)
		}
		if _, err := redis.Do(ctx, "RPUSH", key, string(data)); err != nil {
			return apperr.Wrap(err, apperr.ErrInternal)
		}
	}

	// Keep window: max_window_size * 2 (user + assistant pairs)
	window := readWindowSize(ctx)
	_, _ = redis.Do(ctx, "LTRIM", key, -window*2, -1)

	// Expire
	if _, err := redis.Do(ctx, "EXPIRE", key, ttlSeconds); err != nil {
		return apperr.Wrap(err, apperr.ErrInternal)
	}
	return nil
}

// UpdateSessionTitle updates the session title in MySQL (first user message).
func (s *RedisSessionStore) UpdateSessionTitle(ctx context.Context, tenantID, sessionID, title string) error {
	return s.sessions.UpdateTitle(ctx, tenantID, sessionID, title)
}

// ListSessions returns paginated sessions for a user.
func (s *RedisSessionStore) ListSessions(ctx context.Context, tenantID, userID string, page, size int) ([]domain.SessionSummary, int, error) {
	records, total, err := s.sessions.ListByUser(ctx, tenantID, userID, page, size)
	if err != nil {
		return nil, 0, apperr.Wrap(err, apperr.ErrInternal)
	}
	items := make([]domain.SessionSummary, len(records))
	for i, r := range records {
		items[i] = domain.SessionSummary{
			SessionID: r.SessionID,
			Title:     r.Title,
			AgentType: r.AgentType,
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	return items, total, nil
}

// GetSession retrieves a single session.
func (s *RedisSessionStore) GetSession(ctx context.Context, tenantID, sessionID string) (*domain.SessionSummary, error) {
	record, err := s.sessions.Get(ctx, tenantID, sessionID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if record == nil {
		return nil, apperr.ErrNotFound
	}
	return &domain.SessionSummary{
		SessionID: record.SessionID,
		Title:     record.Title,
		AgentType: record.AgentType,
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func redisKey(tenantID, sessionID string) string {
	return fmt.Sprintf("ws:%s:session:%s:msgs", tenantID, sessionID)
}

func readWindowSize(ctx context.Context) int {
	v := g.Cfg().MustGet(ctx, "session.max_window_size", maxWindowSize).Int()
	if v <= 0 {
		return maxWindowSize
	}
	return v
}

// Ensure RedisSessionStore implements domain.SessionService.
var _ domain.SessionService = (*RedisSessionStore)(nil)

// RedisGetter is a helper to access Redis from the memory package.
func Redis() *gredis.Redis { return g.Redis() }