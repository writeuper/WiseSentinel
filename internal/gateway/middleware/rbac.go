package middleware

import (
	"strings"

	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/net/ghttp"
)

const (
	httpMethodDelete = "DELETE"
	httpMethodPost   = "POST"
	httpMethodGet    = "GET"
)

// RBAC enforces role-based access for protected routes.
func RBAC(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	required := requiredRoles(r.URL.Path, r.Method)
	if len(required) == 0 {
		r.Middleware.Next()
		return
	}

	roles := ctxkeys.RolesFrom(r.Context())
	if hasAnyRole(roles, required) {
		r.Middleware.Next()
		return
	}
	writeError(r, apperr.ErrForbidden)
}

func requiredRoles(path, method string) []string {
	switch {
	case strings.HasPrefix(path, "/api/v1/admin"):
		return []string{"sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/ops"):
		return []string{"operator", "sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodDelete:
		return []string{"sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodPost:
		return []string{"operator", "sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/approvals"):
		return []string{"sre_admin", "platform_admin"}
	default:
		return nil
	}
}

func hasAnyRole(userRoles, required []string) bool {
	set := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		set[role] = struct{}{}
	}
	for _, role := range required {
		if _, ok := set[role]; ok {
			return true
		}
	}
	return false
}