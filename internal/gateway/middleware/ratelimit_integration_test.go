//go:build integration

package middleware

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestSlidingWindowCountsRequestsWithinSameMillisecond(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("OPS_TEST_REDIS_ADDR"))
	if addr == "" {
		t.Skip("OPS_TEST_REDIS_ADDR is required")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect Redis: %v", err)
	}
	key := "ws:test:ratelimit:" + rateLimitMember(time.Now().UnixMilli())
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	nowMs := time.Now().UnixMilli()
	for attempt, want := range []int{0, 0, 1} {
		got, err := client.Eval(ctx, slidingWindowScript, []string{key}, 60, 2, nowMs, rateLimitMember(nowMs)).Int()
		if err != nil {
			t.Fatalf("attempt %d Eval: %v", attempt, err)
		}
		if got != want {
			t.Fatalf("attempt %d result = %d, want %d", attempt, got, want)
		}
	}
	count, err := client.ZCard(ctx, key).Result()
	if err != nil {
		t.Fatalf("ZCARD: %v", err)
	}
	if count != 2 {
		t.Fatalf("same-millisecond accepted member count = %d, want 2", count)
	}
}
