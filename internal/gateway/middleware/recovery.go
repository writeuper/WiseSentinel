package middleware

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// Recovery catches panics and returns a unified error response.
func Recovery(r *ghttp.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			g.Log().Errorf(r.Context(), "panic recovered: %v", rec)
			writeError(r, ErrInternal)
		}
	}()
	r.Middleware.Next()
}