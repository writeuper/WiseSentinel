//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestSessionDeactivateFencesFurtherReadsIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, sessionID := "session-del-"+uuid.NewString(), "sess-"+uuid.NewString()
	repo := NewSessionRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_session").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &Session{TenantID: tenantID, SessionID: sessionID, UserID: "test-user", Title: "test", AgentType: "chat"}); err != nil {
		t.Fatal(err)
	}
	if session, err := repo.Get(ctx, tenantID, sessionID); err != nil || session == nil {
		t.Fatalf("active session = %#v, %v", session, err)
	}
	deleted, err := repo.Deactivate(ctx, tenantID, sessionID)
	if err != nil || !deleted {
		t.Fatalf("deactivate = %v, %v", deleted, err)
	}
	if session, err := repo.Get(ctx, tenantID, sessionID); err != nil || session != nil {
		t.Fatalf("inactive session must be unreadable: %#v, %v", session, err)
	}
	if deleted, err := repo.Deactivate(ctx, tenantID, sessionID); err != nil || deleted {
		t.Fatalf("repeat deactivate = %v, %v; want false, nil", deleted, err)
	}
}

func TestSessionDeactivateFencesTitleMutationIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, sessionID := "session-title-"+uuid.NewString(), "sess-"+uuid.NewString()
	repo := NewSessionRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_session").Where("tenant_id", tenantID).Delete() })
	if err := repo.Create(ctx, &Session{TenantID: tenantID, SessionID: sessionID, UserID: "test-user", Title: "initial", AgentType: "chat"}); err != nil {
		t.Fatal(err)
	}
	if active, err := repo.IsActive(ctx, tenantID, sessionID); err != nil || !active {
		t.Fatalf("new session active = %v, %v", active, err)
	}
	if deleted, err := repo.Deactivate(ctx, tenantID, sessionID); err != nil || !deleted {
		t.Fatalf("deactivate = %v, %v", deleted, err)
	}
	if err := repo.UpdateTitle(ctx, tenantID, sessionID, "must-not-apply"); err != nil {
		t.Fatal(err)
	}
	if active, err := repo.IsActive(ctx, tenantID, sessionID); err != nil || active {
		t.Fatalf("inactive session active = %v, %v", active, err)
	}
	var row struct {
		Title string `json:"title"`
	}
	if err := g.DB().Ctx(ctx).Model("ws_session").Where("tenant_id", tenantID).Where("session_id", sessionID).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if row.Title != "initial" {
		t.Fatalf("inactive title = %q, want initial", row.Title)
	}
}

func TestSessionDeleteAtomicallyRemovesChatTurnsIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, sessionID := "session-turn-delete-"+uuid.NewString(), "sess-"+uuid.NewString()
	sessions, turns := NewSessionRepo(), NewChatTurnRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_chat_turn").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_session").Where("tenant_id", tenantID).Delete()
	})
	if err := sessions.Create(ctx, &Session{TenantID: tenantID, SessionID: sessionID, UserID: "test-user", Title: "test", AgentType: "chat"}); err != nil {
		t.Fatal(err)
	}
	turn := &ChatTurn{TenantID: tenantID, UserID: "test-user", SessionID: sessionID, IdempotencyKey: "idem-" + uuid.NewString(), RequestHash: "aabb", ExecutionToken: uuid.NewString()}
	if _, created, err := turns.Claim(ctx, turn); err != nil || !created {
		t.Fatalf("claim = created=%v, err=%v", created, err)
	}
	if deleted, err := sessions.DeactivateAndDeleteTurns(ctx, tenantID, sessionID); err != nil || !deleted {
		t.Fatalf("delete session = %v, %v", deleted, err)
	}
	if stored, err := turns.Get(ctx, tenantID, "test-user", sessionID, turn.IdempotencyKey); err != nil || stored != nil {
		t.Fatalf("turn after session deletion = %#v, %v", stored, err)
	}
	if session, err := sessions.Get(ctx, tenantID, sessionID); err != nil || session != nil {
		t.Fatalf("session after deletion = %#v, %v", session, err)
	}
}
