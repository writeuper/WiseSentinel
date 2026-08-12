package handler

import (
	"strings"
	"testing"

	v1 "wisesentinel-platform/api/v1"
)

func TestAlertEventProjectionUsesAllowlistAndRedacts(t *testing.T) {
	const canary = "WS_CANARY_ALERT_SECRET_123456789"
	got := alertEventProjection(
		"ops@example.com", "firing", "group-a",
		map[string]string{"service": "checkout", "token": canary},
		map[string]string{"summary": "checkout failed", "internal_debug": canary},
		"https://alerts.example.test/?token="+canary, "4",
		[]v1.AlertEvent{{
			Status:      "firing",
			Labels:      map[string]string{"alertname": "CheckoutDown", "password": canary},
			Annotations: map[string]string{"description": "Bearer " + canary, "secret_note": canary},
			Fingerprint: "fingerprint-a",
		}},
	)
	for _, forbidden := range []string{canary, "internal_debug", "secret_note", `"token"`, `"password"`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("projection leaked %q: %s", forbidden, got)
		}
	}
	for _, expected := range []string{"CheckoutDown", "checkout", "checkout failed", "<redacted:authorization>"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("projection omitted allowed diagnostic %q: %s", expected, got)
		}
	}
}
