//go:build integration

package task

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestOpsLeaseDoesNotReleaseOrRenewAnotherOwnerIntegration(t *testing.T) {
	addr := os.Getenv("OPS_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("OPS_TEST_REDIS_ADDR is not configured")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	key := "ops-lease-itest:" + uuid.NewString()
	t.Cleanup(func() { _ = client.Del(ctx, key).Err() })

	if ok, err := client.SetNX(ctx, key, "token-a", 50*time.Millisecond).Result(); err != nil || !ok {
		t.Fatalf("owner A acquire = %v, %v", ok, err)
	}
	time.Sleep(80 * time.Millisecond)
	if ok, err := client.SetNX(ctx, key, "token-b", time.Second).Result(); err != nil || !ok {
		t.Fatalf("owner B acquire after expiry = %v, %v", ok, err)
	}
	if released, err := client.Eval(ctx, compareDeleteLua, []string{key}, "token-a").Int(); err != nil || released != 0 {
		t.Fatalf("stale owner release = %d, %v; want 0, nil", released, err)
	}
	if value, err := client.Get(ctx, key).Result(); err != nil || value != "token-b" {
		t.Fatalf("lease owner after stale release = %q, %v; want token-b", value, err)
	}
	if renewed, err := client.Eval(ctx, compareExpireLua, []string{key}, "token-a", time.Second.Milliseconds()).Int(); err != nil || renewed != 0 {
		t.Fatalf("stale owner renew = %d, %v; want 0, nil", renewed, err)
	}
	if renewed, err := client.Eval(ctx, compareExpireLua, []string{key}, "token-b", time.Second.Milliseconds()).Int(); err != nil || renewed != 1 {
		t.Fatalf("current owner renew = %d, %v; want 1, nil", renewed, err)
	}
}
