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
	httpMethodPut    = "PUT"
)

// RBAC enforces role-based access for protected routes.
func RBAC(r *ghttp.Request) {
	if isPublicPath(r.URL.Path) || isWebhookPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}

	required := requiredRoles(r.URL.Path, r.Method)
	if ctxkeys.AuthMethodFrom(r.Context()) == "service_api_key" {
		if scope := requiredScope(r.URL.Path, r.Method); scope != "" && !hasScope(ctxkeys.ScopesFrom(r.Context()), scope) {
			writeError(r, apperr.ErrForbidden)
			return
		}
	}
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

// requiredScope is applied only to fixed service identities. Human JWT
// users continue to use role/RBAC policy, while workload keys must opt into
// each higher-risk API explicitly.
func requiredScope(path, method string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/admin/agent-configs") && method == httpMethodGet:
		return "agent_config:read"
	case strings.HasPrefix(path, "/api/v1/admin/agent-configs") && method == httpMethodPut:
		return "agent_config:write"
	case strings.HasPrefix(path, "/api/v1/admin"):
		return "admin:write"
	case strings.HasPrefix(path, "/api/v1/ops") && method == httpMethodGet:
		return "ops:read"
	case strings.HasPrefix(path, "/api/v1/ops"):
		return "ops:execute"
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodGet:
		return "knowledge:read"
	case strings.HasPrefix(path, "/api/v1/knowledge"):
		return "knowledge:write"
	case strings.HasPrefix(path, "/api/v1/approvals") && method == httpMethodGet:
		return "approval:read"
	case strings.HasPrefix(path, "/api/v1/approvals"):
		return "approval:decide"
	case strings.HasPrefix(path, "/api/v1/traces"):
		return "trace:read"
	default:
		return ""
	}
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func requiredRoles(path, method string) []string {
	switch {
	case strings.HasPrefix(path, "/api/v1/admin"):
		return []string{"sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/ops"):
		return []string{"operator", "sre_admin", "platform_admin"}
	case strings.HasPrefix(path, "/api/v1/knowledge") && method == httpMethodDelete:
		return []string{"operator", "sre_admin", "platform_admin"}
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
