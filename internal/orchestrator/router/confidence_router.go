package router

import (
	"context"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

// ConfidenceRouter wraps a rule-based router with a RAG confidence probe.
//
// Routing policy (mirrors §6.1 of the refactor design doc):
//   - Explicit /ops path or alert-like query  → Ops Agent (no RAG probe needed)
//   - Explicit /knowledge path                → Knowledge Agent
//   - Otherwise (chat-bound): probe RAG
//   - High confidence (≥0.75)                  → Chat Agent (RAG fast answer)
//   - Mid (0.5~0.75) / Low (<0.5)              → Ops Agent (Agentic Search)
//
// When RAG is unavailable the router falls back to the rule-based decision so
// the platform keeps working in degraded mode.
type ConfidenceRouter struct {
	rules     *RuleRouter
	rag       domain.RAGService
	probeTopK int
}

// NewConfidenceRouter creates a router that augments rule routing with a
// RAG confidence probe for non-alert, non-ops chat queries.
func NewConfidenceRouter(rag domain.RAGService) *ConfidenceRouter {
	return &ConfidenceRouter{
		rules:     NewRuleRouter(),
		rag:       rag,
		probeTopK: 3,
	}
}

// Route selects the target agent type.
func (r *ConfidenceRouter) Route(ctx context.Context, req *domain.RouteRequest) (domain.AgentType, error) {
	if req.AgentType != "" {
		return req.AgentType, nil
	}

	// 1. Explicit knowledge path is always Knowledge Agent.
	if hasPrefix(req.Path, "/knowledge") {
		return domain.AgentTypeKnowledge, nil
	}

	// 2. Explicit ops path or alert-like query → Ops Agent directly.
	if hasPrefix(req.Path, "/ops") || alertPattern.MatchString(req.Query) {
		return domain.AgentTypeOps, nil
	}

	// 3. Chat-bound query: probe RAG confidence to decide chat vs ops.
	if r.rag == nil {
		return domain.AgentTypeChat, nil // degraded mode, no RAG
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	resp, err := r.rag.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID: tenantID,
		Query:    req.Query,
		TopK:     r.probeTopK,
	})
	if err != nil || resp == nil {
		// RAG probe failed; let the chat agent handle it (it will retry RAG).
		return domain.AgentTypeChat, nil
	}

	switch resp.Confidence {
	case domain.ConfidenceHigh:
		// Historical knowledge is strong enough for a fast RAG answer.
		return domain.AgentTypeChat, nil
	case domain.ConfidenceMid, domain.ConfidenceLow:
		// Unknown / weak-match question → Agentic Search.
		return domain.AgentTypeOps, nil
	default:
		return domain.AgentTypeChat, nil
	}
}
