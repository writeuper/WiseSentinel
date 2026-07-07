package middleware

import (
	"net/http"
	"strings"

	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/response"

	"github.com/gogf/gf/v2/net/ghttp"
)

// UnifiedResponse wraps handler output in the standard envelope.
func UnifiedResponse(r *ghttp.Request) {
	r.Middleware.Next()

	if r.Response.BufferLength() > 0 {
		return
	}

	path := r.URL.Path
	if strings.HasPrefix(path, "/health") || path == "/metrics" {
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
	if ae, ok := err.(*apperr.AppError); ok {
		r.Response.Status = ae.HTTP
		r.Response.WriteJson(response.Fail(ae.Code, ae.Message))
		return
	}
	r.Response.Status = http.StatusInternalServerError
	r.Response.WriteJson(response.Fail(apperr.ErrInternal.Code, err.Error()))
}

func writeError(r *ghttp.Request, ae *apperr.AppError) {
	r.Response.Status = ae.HTTP
	r.Response.WriteJson(response.Fail(ae.Code, ae.Message))
}

// ErrInternal is a shortcut for middleware-level errors.
var ErrInternal = apperr.New(50001, 500, "internal server error")