package middleware

import (
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// TenantResolver resolves tenant from header with default fallback.
func TenantResolver(r *ghttp.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = g.Cfg().MustGet(r.Context(), "tenant.default_id", domain.DefaultTenantID).String()
	}
	ctx := ctxkeys.WithTenantID(r.Context(), tenantID)
	r.SetCtx(ctx)
	r.Middleware.Next()
}