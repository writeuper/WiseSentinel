package middleware

import (
	"fmt"

	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// Recovery catches panics and returns a unified error response.
func Recovery(r *ghttp.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			g.Log().Errorf(r.Context(), "panic recovered: %s", redact.Summary(fmt.Sprint(rec), 1000))
			writeError(r, ErrInternal)
		}
	}()
	r.Middleware.Next()
}
