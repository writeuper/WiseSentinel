package middleware

import (
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// RateLimit applies per-user request throttling using Redis sliding-window algorithm.
//
// Uses a ZSET-based sliding window to avoid the INCR+EXPIRE race condition:
//  1. ZADD with the current millisecond timestamp
//  2. ZREMRANGEBYSCORE to evict entries older than 60 seconds
//  3. ZCARD to count the entries in the current window
//  4. EXPIRE to auto-clean after 120 seconds of inactivity
func RateLimit(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	ctx := r.Context()
	userID := ctxkeys.UserIDFrom(ctx)
	if userID == "" {
		userID = "anonymous"
	}

	limitKey := rateLimitKey(r.URL.Path)
	if limitKey == "" {
		r.Middleware.Next()
		return
	}

	maxPerMinute := g.Cfg().MustGet(ctx, limitKey, 60).Int()
	redisKey := "ws:" + ctxkeys.TenantIDFrom(ctx) + ":ratelimit:" + userID + ":" + limitKey

	// Lua script: sliding-window rate limiter
	// KEYS[1] = redisKey
	// ARGV[1] = window size (seconds)
	// ARGV[2] = max count
	// ARGV[3] = current time (ms)
	const slidingWindowScript = `
		local key = KEYS[1]
		local window = tonumber(ARGV[1])
		local maxCount = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local cutoff = now - (window * 1000)

		redis.call("ZREMRANGEBYSCORE", key, 0, cutoff)
		local count = redis.call("ZCARD", key)

		if count >= maxCount then
			return 1
		end

		redis.call("ZADD", key, now, tostring(now))
		redis.call("EXPIRE", key, window * 2)
		return 0
	`

	nowMs := time.Now().UnixMilli()
	result, err := g.Redis().Do(ctx, "EVAL", slidingWindowScript, 1, redisKey, 60, maxPerMinute, nowMs)
	if err != nil {
		// If Redis is down, allow the request through.
		r.Middleware.Next()
		return
	}
	if result.Int() == 1 {
		writeError(r, apperr.ErrRateLimited)
		return
	}
	r.Middleware.Next()
}

func rateLimitKey(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/chat"):
		return "rate_limit.chat_per_minute"
	case strings.HasPrefix(path, "/api/v1/ops"):
		return "rate_limit.ops_per_minute"
	default:
		return ""
	}
}