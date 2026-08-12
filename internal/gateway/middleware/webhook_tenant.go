package middleware

import (
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/net/ghttp"
)

// WebhookTenant injects the configured single tenant and ignores external tenant headers.
func WebhookTenant(r *ghttp.Request) {
	tenantID := strings.TrimSpace(configx.String(r.Context(), "webhook.alertmanager.tenant_id", "ALERTMANAGER_WEBHOOK_TENANT_ID"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(configx.String(r.Context(), "tenant.default_id", ""))
	}
	if tenantID == "" {
		tenantID = domain.DefaultTenantID
	}
	r.SetCtx(ctxkeys.WithTenantID(r.Context(), tenantID))
	r.Middleware.Next()
}
