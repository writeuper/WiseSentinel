package adapters

import (
	"fmt"
	"net/http"

	"wisesentinel-platform/internal/pkg/redact"
)

// upstreamHTTPError deliberately excludes an upstream response body. Response
// bodies can echo credentials or request data; a request ID is sufficient for
// safe support correlation.
func upstreamHTTPError(provider string, resp *http.Response) error {
	requestID := redact.Summary(resp.Header.Get("X-Request-ID"), 128)
	if requestID == "" {
		requestID = redact.Summary(resp.Header.Get("X-Amzn-Requestid"), 128)
	}
	if requestID == "" {
		return fmt.Errorf("%s HTTP %d", provider, resp.StatusCode)
	}
	return fmt.Errorf("%s HTTP %d request_id=%s", provider, resp.StatusCode, requestID)
}
