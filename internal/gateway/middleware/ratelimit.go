package middleware

import (
	"strconv"
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
)

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

	redis.call("ZADD", key, now, ARGV[4])
	redis.call("EXPIRE", key, window * 2)
	return 0
`

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
	userID := strings.TrimSpace(ctxkeys.UserIDFrom(ctx))
	if userID == "" {
		userID = "anonymous"
	}

	limitKey := rateLimitKey(r.Method, r.URL.Path)
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
	// ARGV[4] = unique request member
	nowMs := time.Now().UnixMilli()
	result, err := g.Redis().Do(ctx, "EVAL", slidingWindowScript, 1, redisKey, 60, maxPerMinute, nowMs, rateLimitMember(nowMs))
	if err != nil {
		// Agent, Ops, and signed Webhook paths can trigger expensive downstream
		// execution. Failing open here converts a Redis outage into unbounded
		// model/tool load, so these configured paths fail closed.
		if rateLimitFailsClosed(limitKey) {
			writeError(r, apperr.ErrRateLimitUnavailable)
			return
		}
		r.Middleware.Next()
		return
	}
	if result.Int() == 1 {
		writeError(r, apperr.ErrRateLimited)
		return
	}
	r.Middleware.Next()
}

func rateLimitFailsClosed(limitKey string) bool {
	switch limitKey {
	case "rate_limit.chat_per_minute", "rate_limit.ops_per_minute", "rate_limit.webhook_per_minute":
		return true
	default:
		return false
	}
}

func rateLimitMember(nowMs int64) string {
	return strconv.FormatInt(nowMs, 10) + ":" + uuid.NewString()
}

func rateLimitKey(method, path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/chat"):
		return "rate_limit.chat_per_minute"
	case method == "POST" && path == "/api/v1/ops/analyze":
		return "rate_limit.ops_per_minute"
	case method == "POST" && path == "/internal/webhooks/alertmanager":
		return "rate_limit.webhook_per_minute"
	default:
		return ""
	}
}
