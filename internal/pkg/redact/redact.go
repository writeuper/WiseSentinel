// Package redact creates safe diagnostic projections of potentially sensitive data.
// It must only be used at observability and presentation boundaries; callers that
// execute tools or invoke models keep their original payloads.
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var sensitiveKey = map[string]struct{}{
	"access_token": {}, "api_key": {}, "apikey": {}, "authorization": {},
	"client_secret": {}, "cookie": {}, "credential": {}, "credentials": {},
	"connection_string": {}, "dsn": {},
	"password": {}, "passwd": {}, "private_key": {}, "refresh_token": {},
	"secret": {}, "set-cookie": {}, "token": {},
}

var textRules = []struct {
	re   *regexp.Regexp
	mark string
}{
	{regexp.MustCompile(`(?i)\b(?:authorization\s*:\s*)?(?:bearer|basic)\s+[a-z0-9._~+/=-]{8,}`), "<redacted:authorization>"},
	{regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\b`), "<redacted:jwt>"},
	{regexp.MustCompile(`\bsk-[a-zA-Z0-9_-]{12,}\b`), "<redacted:api-key>"},
	{regexp.MustCompile(`(?i)(?:mysql|postgres(?:ql)?|redis)://[^\s/@:]+:[^\s/@]+@`), "<redacted:dsn>@"},
	{regexp.MustCompile(`(?i)\b[^\s:@]+:[^\s@]+@tcp\([^)]*\)/[^\s]+`), "<redacted:dsn>"},
	{regexp.MustCompile(`(?i)([?&](?:access_token|api_key|apikey|password|secret|signature|sig|token)=)[^&#\s]+`), "$1<redacted:query-secret>"},
	{regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`), "<redacted:email>"},
}

// Text removes credentials and direct identifiers from arbitrary diagnostic text.
func Text(value string) string {
	for _, rule := range textRules {
		value = rule.re.ReplaceAllString(value, rule.mark)
	}
	return value
}

// Summary returns a redacted UTF-8-safe diagnostic value with a bounded length.
func Summary(value string, maxRunes int) string {
	value = Text(value)
	if maxRunes <= 0 {
		return value
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "...(truncated)"
}

// TelemetryProjection is a fail-closed projection for logs, traces and
// durable diagnostic records. Those planes are not an evidence vault: they
// must never retain arbitrary user prompts, document bodies, model output or
// tool payloads. The original value stays in memory for the authorized
// execution path only; telemetry keeps just a size signal useful for incident
// correlation and capacity diagnosis.
func TelemetryProjection(value string) string {
	return fmt.Sprintf(`{"suppressed":true,"bytes":%d}`, len([]byte(value)))
}

// JSON returns a structurally valid redacted JSON projection when possible. For
// non-JSON input it returns a redacted text projection instead.
func JSON(value string) string {
	var data any
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return Text(value)
	}
	data = redactJSONValue(data, false)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(data)
	if err != nil {
		return "<redacted:unserializable-json>"
	}
	return strings.TrimSpace(encoded.String())
}

func redactJSONValue(value any, redactValue bool) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			_, sensitive := sensitiveKey[strings.ToLower(strings.TrimSpace(key))]
			out[key] = redactJSONValue(child, redactValue || sensitive)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactJSONValue(child, redactValue)
		}
		return out
	case string:
		if redactValue {
			return "<redacted:secret>"
		}
		return Text(typed)
	default:
		if redactValue {
			return "<redacted:secret>"
		}
		return value
	}
}
