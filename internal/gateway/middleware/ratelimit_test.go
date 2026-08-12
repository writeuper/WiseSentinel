package middleware

import (
	"strings"
	"testing"
)

func TestRateLimitMemberIsUniqueWithinSameMillisecond(t *testing.T) {
	first := rateLimitMember(123)
	second := rateLimitMember(123)
	if first == second {
		t.Fatal("same-millisecond requests must not share a ZSET member")
	}
	if !strings.HasPrefix(first, "123:") || !strings.HasPrefix(second, "123:") {
		t.Fatalf("members must retain their timestamp prefix: %q, %q", first, second)
	}
}

func TestRateLimitKeyIncludesSignedWebhook(t *testing.T) {
	if got := rateLimitKey("POST", "/internal/webhooks/alertmanager"); got != "rate_limit.webhook_per_minute" {
		t.Fatalf("webhook rate limit key = %q", got)
	}
}

func TestRateLimitedAgentEntrypointsFailClosed(t *testing.T) {
	for _, key := range []string{"rate_limit.chat_per_minute", "rate_limit.ops_per_minute", "rate_limit.webhook_per_minute"} {
		if !rateLimitFailsClosed(key) {
			t.Fatalf("%s must fail closed when Redis is unavailable", key)
		}
	}
	if rateLimitFailsClosed("rate_limit.unknown") {
		t.Fatal("unknown rate limit class must not silently inherit a policy")
	}
}
