package middleware

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/pkg/trace"
)

func TestValidTraceIDAcceptsPlatformAndSafeUpstreamFormats(t *testing.T) {
	for _, value := range []string{trace.NewID(), "upstream-01_abc.def"} {
		if !isValidTraceID(value) {
			t.Fatalf("trace ID %q should be accepted", value)
		}
	}
}

func TestValidTraceIDRejectsBoundaryAndUnsafeValues(t *testing.T) {
	for _, value := range []string{
		"",
		strings.Repeat("a", maxTraceIDLength+1),
		"trace id with spaces",
		"tenant-a/query=credential",
		"trace\nforged-log-entry",
		"中文链路",
	} {
		if isValidTraceID(value) {
			t.Fatalf("trace ID %q should be rejected", value)
		}
	}
}
