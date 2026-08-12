package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/net/ghttp"
)

const (
	defaultWebhookWindow       = 300 * time.Second
	defaultWebhookMaxBodyBytes = 1 << 20 // 1 MiB
)

var errWebhookBodyTooLarge = errors.New("webhook payload exceeds maximum size")

// AlertmanagerSignature validates X-WiseSentinel-Timestamp and HMAC-SHA256
// over timestamp + "." + the exact request body.
func AlertmanagerSignature(r *ghttp.Request) {
	secret := strings.TrimSpace(configx.String(r.Context(), "webhook.alertmanager.secret", "ALERTMANAGER_WEBHOOK_SECRET"))
	timestamp := strings.TrimSpace(r.Header.Get("X-WiseSentinel-Timestamp"))
	signature := strings.TrimSpace(r.Header.Get("X-WiseSentinel-Signature"))
	body, err := readWebhookBody(r.Body, webhookMaxBodyBytes(r))
	if errors.Is(err, errWebhookBodyTooLarge) {
		writeWebhookPayloadTooLarge(r)
		return
	}
	if secret == "" || err != nil || !VerifyWebhookSignature(timestamp, signature, body, secret, time.Now(), webhookWindow(r)) {
		writeWebhookAuthError(r)
		return
	}

	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.SetCtx(ctxkeys.WithWebhookBody(r.Context(), body))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	provided := signature
	if strings.HasPrefix(provided, "sha256=") {
		provided = strings.TrimPrefix(provided, "sha256=")
	}
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(strings.ToLower(provided)), []byte(expected)) != 1 {
		writeWebhookAuthError(r)
		return
	}
	r.Middleware.Next()
}

func VerifyWebhookSignature(timestamp, signature string, body []byte, secret string, now time.Time, window time.Duration) bool {
	unix, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil || now.Sub(time.Unix(unix, 0)) > window || time.Unix(unix, 0).Sub(now) > window {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	provided := strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")
	return len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(strings.ToLower(provided)), []byte(expected)) == 1
}

func webhookWindow(r *ghttp.Request) time.Duration {
	if value := strings.TrimSpace(configx.String(r.Context(), "webhook.alertmanager.window_seconds", "ALERTMANAGER_WEBHOOK_WINDOW_SECONDS")); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultWebhookWindow
}

func webhookMaxBodyBytes(r *ghttp.Request) int64 {
	if value := strings.TrimSpace(configx.String(r.Context(), "webhook.alertmanager.max_body_bytes", "ALERTMANAGER_WEBHOOK_MAX_BODY_BYTES")); value != "" {
		if bytes, err := strconv.ParseInt(value, 10, 64); err == nil && bytes > 0 {
			return bytes
		}
	}
	return defaultWebhookMaxBodyBytes
}

func readWebhookBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultWebhookMaxBodyBytes
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errWebhookBodyTooLarge
	}
	return body, nil
}

func writeWebhookAuthError(r *ghttp.Request) {
	r.Response.Status = http.StatusUnauthorized
	r.Response.WriteJson(map[string]any{"code": http.StatusUnauthorized, "message": "invalid webhook signature"})
}

func writeWebhookPayloadTooLarge(r *ghttp.Request) {
	r.Response.Status = http.StatusRequestEntityTooLarge
	r.Response.WriteJson(map[string]any{"code": http.StatusRequestEntityTooLarge, "message": "webhook payload too large"})
}
