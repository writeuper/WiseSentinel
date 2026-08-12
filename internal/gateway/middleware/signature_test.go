package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifyWebhookSignature(t *testing.T) {
	old := os.Getenv("ALERTMANAGER_WEBHOOK_SECRET")
	defer os.Setenv("ALERTMANAGER_WEBHOOK_SECRET", old)
	_ = os.Setenv("ALERTMANAGER_WEBHOOK_SECRET", "test-secret")
	body := []byte(`{"status":"firing"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	if !VerifyWebhookSignature(timestamp, signature, body, "test-secret", time.Now(), 300*time.Second) {
		t.Fatal("valid signature rejected")
	}
	if VerifyWebhookSignature(timestamp, strings.Repeat("0", 64), body, "test-secret", time.Now(), 300*time.Second) {
		t.Fatal("invalid signature accepted")
	}
	if VerifyWebhookSignature(strconv.FormatInt(time.Now().Add(-301*time.Second).Unix(), 10), signature, body, "test-secret", time.Now(), 300*time.Second) {
		t.Fatal("expired signature accepted")
	}
	if VerifyWebhookSignature(timestamp, signature, body, "test-secret", time.Now().Add(301*time.Second), 300*time.Second) {
		t.Fatal("future/replayed signature accepted")
	}
}

func TestReadWebhookBodyHonorsConfiguredLimit(t *testing.T) {
	body, err := readWebhookBody(bytes.NewBufferString("1234"), 4)
	if err != nil || string(body) != "1234" {
		t.Fatalf("exact limit body = %q, %v", body, err)
	}
	if _, err := readWebhookBody(bytes.NewBufferString("12345"), 4); !errors.Is(err, errWebhookBodyTooLarge) {
		t.Fatalf("oversized body error = %v, want errWebhookBodyTooLarge", err)
	}
}
