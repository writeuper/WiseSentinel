//go:build integration

package repository

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestChatTurnClaimIsTenantUserSessionScopedAndFencedIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "chat-turn-" + uuid.NewString()
	userID, sessionID := "user-a", "sess-"+uuid.NewString()
	key := "idem-" + uuid.NewString()
	repo := NewChatTurnRepo()
	t.Cleanup(func() { _, _ = g.DB().Ctx(ctx).Model("ws_chat_turn").Where("tenant_id", tenantID).Delete() })

	var winners atomic.Int32
	var owner *ChatTurn
	var ownerMu sync.Mutex
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			candidate := &ChatTurn{TenantID: tenantID, UserID: userID, SessionID: sessionID, IdempotencyKey: key, RequestHash: "aabb", ExecutionToken: uuid.NewString()}
			turn, created, err := repo.Claim(ctx, candidate)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if created {
				winners.Add(1)
				ownerMu.Lock()
				owner = turn
				ownerMu.Unlock()
			}
		}()
	}
	group.Wait()
	if winners.Load() != 1 || owner == nil {
		t.Fatalf("created owners = %d, owner=%#v", winners.Load(), owner)
	}

	if succeeded, err := repo.Succeed(ctx, owner, `{"session_id":"`+sessionID+`","answer":"safe"}`, "trace-safe"); err != nil || !succeeded {
		t.Fatalf("owner succeed = %v, %v", succeeded, err)
	}
	if succeeded, err := repo.Succeed(ctx, &ChatTurn{TenantID: tenantID, UserID: userID, SessionID: sessionID, IdempotencyKey: key, ExecutionToken: "stale"}, `{}`, "trace-stale"); err != nil || succeeded {
		t.Fatalf("stale succeed = %v, %v; want false, nil", succeeded, err)
	}

	replay, created, err := repo.Claim(ctx, &ChatTurn{TenantID: tenantID, UserID: userID, SessionID: sessionID, IdempotencyKey: key, RequestHash: "aabb", ExecutionToken: uuid.NewString()})
	if err != nil || created || replay == nil || replay.Status != ChatTurnSucceeded || replay.ResponseJSON == "" {
		t.Fatalf("replay claim = %#v, created=%v, err=%v", replay, created, err)
	}
	otherUser, created, err := repo.Claim(ctx, &ChatTurn{TenantID: tenantID, UserID: "user-b", SessionID: sessionID, IdempotencyKey: key, RequestHash: "aabb", ExecutionToken: uuid.NewString()})
	if err != nil || !created || otherUser == nil {
		t.Fatalf("different user must not share turn: %#v, created=%v, err=%v", otherUser, created, err)
	}
}
