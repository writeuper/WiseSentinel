// Package router implements intent routing for multi-agent orchestration.
package router

import (
	"context"
	"regexp"
	"strings"

	"wisesentinel-platform/internal/domain"
)

var alertPattern = regexp.MustCompile(`(?i)告警|alert|firing|故障|异常|down|offline`)

// RuleRouter routes requests using Phase 1 rule-based logic.
type RuleRouter struct{}

func NewRuleRouter() *RuleRouter {
	return &RuleRouter{}
}

func (r *RuleRouter) Route(ctx context.Context, req *domain.RouteRequest) (domain.AgentType, error) {
	if req.AgentType != "" {
		return req.AgentType, nil
	}
	switch {
	case hasPrefix(req.Path, "/ops"):
		return domain.AgentTypeOps, nil
	case hasPrefix(req.Path, "/knowledge"):
		return domain.AgentTypeKnowledge, nil
	case alertPattern.MatchString(req.Query):
		return domain.AgentTypeOps, nil
	default:
		return domain.AgentTypeChat, nil
	}
}

func hasPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix) || strings.HasPrefix(path, "/api/v1"+prefix)
}
