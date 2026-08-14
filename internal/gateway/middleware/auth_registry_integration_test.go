//go:build integration

package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"wisesentinel-platform/internal/pkg/ctxkeys"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestServiceAPIKeyRegistryEnforcesStatusExpiryAndIdentityIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	t.Setenv("SERVICE_API_KEY_REGISTRY_ENABLED", "true")
	t.Setenv("SERVICE_API_KEY", "")
	t.Setenv("SERVICE_API_KEY_SHA256", "")
	t.Setenv("SERVICE_API_KEY_PREVIOUS_SHA256", "")
	keyID := "registry-itest-" + uuid.NewString()
	key := "registry-secret-" + uuid.NewString()
	digest := sha256.Sum256([]byte(key))
	tenantID := "registry-tenant-" + uuid.NewString()
	if _, err := g.DB().Model("ws_service_api_key").Data(g.Map{
		"key_id": keyID, "key_hash": hex.EncodeToString(digest[:]), "tenant_id": tenantID,
		"user_id": "svc", "roles_json": `["operator"]`, "scopes_json": `["ops:execute"]`,
		"status": "active", "expires_at": time.Now().Add(time.Hour),
	}).Insert(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = g.DB().Model("ws_service_api_key").Where("key_id", keyID).Delete() })
	ctx, ok := withServiceAPIKeyIdentity(context.Background(), key)
	if !ok || ctxkeys.TenantIDFrom(ctx) != tenantID || ctxkeys.AuthKeyIDFrom(ctx) != keyID {
		t.Fatalf("registry identity rejected or misbound: ok=%t tenant=%q key_id=%q", ok, ctxkeys.TenantIDFrom(ctx), ctxkeys.AuthKeyIDFrom(ctx))
	}
	if _, err := g.DB().Model("ws_service_api_key").Where("key_id", keyID).Data(g.Map{"status": "revoked"}).Update(); err != nil {
		t.Fatal(err)
	}
	if _, ok := withServiceAPIKeyIdentity(context.Background(), key); ok {
		t.Fatal("revoked registry key accepted")
	}
}
