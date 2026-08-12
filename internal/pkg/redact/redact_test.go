package redact

import (
	"strings"
	"testing"
)

func TestJSONRedactsNestedSensitiveValuesAndKeepsStructure(t *testing.T) {
	canary := "WS_CANARY_SECRET_123456789"
	got := JSON(`{"query":"checkout 500","headers":{"Authorization":"Bearer ` + canary + `"},"nested":[{"password":"` + canary + `"}]}`)
	if strings.Contains(got, canary) {
		t.Fatalf("redacted JSON leaked canary: %s", got)
	}
	if !strings.Contains(got, `"query":"checkout 500"`) || !strings.Contains(got, "<redacted:secret>") {
		t.Fatalf("unexpected JSON projection: %s", got)
	}
}

func TestTextRedactsCredentialsAndDirectIdentifiers(t *testing.T) {
	value := "Authorization: Bearer WS_CANARY_BEARER_1234567890 contact user@example.com dsn mysql://ws:WS_CANARY_DSN@db:3306/app?token=WS_CANARY_TOKEN go_dsn ws:WS_CANARY_GO_DSN@tcp(db:3306)/app"
	got := Text(value)
	for _, secret := range []string{"WS_CANARY_BEARER_1234567890", "user@example.com", "WS_CANARY_DSN", "WS_CANARY_TOKEN", "WS_CANARY_GO_DSN"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text leaked %q: %s", secret, got)
		}
	}
}

func TestSummaryRedactsBeforeTruncating(t *testing.T) {
	secret := "sk-WS_CANARY_SECRET_1234567890"
	got := Summary(strings.Repeat("x", 20)+secret, 30)
	if strings.Contains(got, secret) || strings.Contains(got, "WS_CANARY") {
		t.Fatalf("summary leaked secret: %s", got)
	}
}

func TestTelemetryProjectionNeverContainsFreeText(t *testing.T) {
	canary := `{"password":"WS_TELEMETRY_CANARY_123456789","content":"customer incident body"}`
	got := TelemetryProjection(canary)
	if strings.Contains(got, "WS_TELEMETRY_CANARY") || strings.Contains(got, "customer incident") {
		t.Fatalf("telemetry projection leaked free text: %s", got)
	}
	if !strings.Contains(got, `"suppressed":true`) || !strings.Contains(got, `"bytes":`) {
		t.Fatalf("unexpected telemetry projection: %s", got)
	}
}
