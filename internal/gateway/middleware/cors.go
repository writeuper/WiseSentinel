package middleware

import (
	"github.com/gogf/gf/v2/net/ghttp"
)

// CORS handles cross-origin requests for the portal.
func CORS(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}