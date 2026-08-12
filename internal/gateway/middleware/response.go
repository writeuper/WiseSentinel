package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/pkg/response"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
)

// UnifiedResponse wraps handler output in the standard envelope.
func UnifiedResponse(r *ghttp.Request) {
	r.Middleware.Next()

	if r.Response.BufferLength() > 0 {
		return
	}

	path := r.URL.Path
	contentType := r.Response.Header().Get("Content-Type")
	if strings.HasPrefix(path, "/health") || path == "/metrics" || strings.HasPrefix(contentType, "text/event-stream") {
		return
	}

	err := r.GetError()
	if err != nil {
		writeHandlerError(r, err)
		return
	}

	res := r.GetHandlerResponse()
	r.Response.WriteJson(response.OK(res))
}

func writeHandlerError(r *ghttp.Request, err error) {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		r.Response.Status = ae.HTTP
		if seconds := retryAfterSeconds(ae.Code); seconds > 0 {
			r.Response.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
		r.Response.WriteJson(response.Fail(ae.Code, ae.Message))
		return
	}
	// GoFrame validates request structs before entering handlers. Translate its
	// framework validation error to the platform's stable client contract and
	// never expose field names or framework-local English messages.
	if isValidationError(err) {
		r.Response.Status = apperr.ErrBadRequest.HTTP
		r.Response.WriteJson(response.Fail(apperr.ErrBadRequest.Code, apperr.ErrBadRequest.Message))
		return
	}
	r.Response.Status = http.StatusInternalServerError
	// Unknown errors can carry provider response bodies. Never echo those raw
	// values to an API client; retain only a redacted diagnostic projection.
	r.Response.WriteJson(response.Fail(apperr.ErrInternal.Code, redact.Summary(err.Error(), 1000)))
}

func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	return gerror.Code(err).Code() == gcode.CodeValidationFailed.Code()
}

// retryAfterSeconds exposes a bounded retry hint only for errors that are
// explicitly safe to retry. It must not be inferred for write/approval paths.
func retryAfterSeconds(code int) int {
	if code == apperr.ErrModelOverloaded.Code {
		return 2
	}
	return 0
}

func writeError(r *ghttp.Request, ae *apperr.AppError) {
	r.Response.Status = ae.HTTP
	r.Response.WriteJson(response.Fail(ae.Code, ae.Message))
}

// ErrInternal is a shortcut for middleware-level errors.
var ErrInternal = apperr.New(50001, 500, "internal server error")
