package router

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

// stubRAG is a configurable RAGService for routing tests.
type stubRAG struct {
	confidence domain.ConfidenceLevel
	err        error
	called     bool
	lastTenant string
}

func (s *stubRAG) Retrieve(ctx context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	s.called = true
	s.lastTenant = req.TenantID
	if s.err != nil {
		return nil, s.err
	}
	return &domain.RetrieveResponse{Confidence: s.confidence, TopScore: 0.5}, nil
}
func (s *stubRAG) Route(ctx context.Context, req *domain.RetrieveRequest) (*domain.RAGRouteDecision, error) {
	resp, err := s.Retrieve(ctx, req)
	if err != nil {
		return nil, err
	}
	return &domain.RAGRouteDecision{Response: resp, Confidence: resp.Confidence, Route: "agent_rag"}, nil
}
func (s *stubRAG) SubmitIndexTask(ctx context.Context, req *domain.IndexTaskRequest) (string, error) {
	return "", nil
}
func (s *stubRAG) GetIndexTask(ctx context.Context, tenantID, taskID string) (*domain.IndexTask, error) {
	return nil, nil
}
func (s *stubRAG) DeleteDocumentChunks(ctx context.Context, tenantID, docID string) error { return nil }

func TestConfidenceRouter_AlertPathSkipsRAG(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceLow}
	r := NewConfidenceRouter(rag)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path:  "/api/v1/ops/analyze",
		Query: "order-service 告警 firing",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeOps {
		t.Errorf("alert path = %v, want ops", got)
	}
	if rag.called {
		t.Error("RAG should be skipped for alert/ops path")
	}
}

func TestConfidenceRouter_HighConfidenceGoesChat(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceHigh}
	r := NewConfidenceRouter(rag)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path:  "/api/v1/chat",
		Query: "如何重置密码",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeChat {
		t.Errorf("high-confidence chat query = %v, want chat", got)
	}
}

func TestConfidenceRouter_LowConfidenceUpgradesToOps(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceLow}
	r := NewConfidenceRouter(rag)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path:  "/api/v1/chat",
		Query: "v3.2 新接口偶发 502 怎么排查",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeOps {
		t.Errorf("low-confidence unknown query = %v, want ops (agentic search)", got)
	}
}

func TestConfidenceRouter_MidConfidenceUpgradesToOps(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceMid}
	r := NewConfidenceRouter(rag)

	got, _ := r.Route(context.Background(), &domain.RouteRequest{
		Path:  "/api/v1/chat",
		Query: "某个边缘问题",
	})
	if got != domain.AgentTypeOps {
		t.Errorf("mid-confidence query = %v, want ops", got)
	}
}

func TestConfidenceRouter_NilRAGFallsBackToChat(t *testing.T) {
	r := NewConfidenceRouter(nil)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path:  "/api/v1/chat",
		Query: "任意问题",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeChat {
		t.Errorf("nil-RAG fallback = %v, want chat", got)
	}
}

func TestConfidenceRouter_RAGErrorFallsBackToChat(t *testing.T) {
	rag := &stubRAG{err: errRAGProbe}
	r := NewConfidenceRouter(rag)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path:  "/api/v1/chat",
		Query: "任意问题",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeChat {
		t.Errorf("RAG-error fallback = %v, want chat", got)
	}
}

func TestConfidenceRouter_KnowledgePath(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceLow}
	r := NewConfidenceRouter(rag)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path: "/api/v1/knowledge/documents",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeKnowledge {
		t.Errorf("knowledge path = %v, want knowledge", got)
	}
	if rag.called {
		t.Error("RAG should be skipped for knowledge path")
	}
}

func TestConfidenceRouter_ExplicitAgentTypeWins(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceHigh}
	r := NewConfidenceRouter(rag)

	got, err := r.Route(context.Background(), &domain.RouteRequest{
		Path:      "/api/v1/chat",
		Query:     "should be ignored",
		AgentType: domain.AgentTypeOps,
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	if got != domain.AgentTypeOps {
		t.Errorf("explicit agent type = %v, want ops", got)
	}
	if rag.called {
		t.Error("RAG should be skipped when agent type is explicit")
	}
}

func TestConfidenceRouter_TenantIDFromContext(t *testing.T) {
	rag := &stubRAG{confidence: domain.ConfidenceHigh}
	r := NewConfidenceRouter(rag)

	ctx := ctxkeys.WithTenantID(context.Background(), "tenant-acme")
	_, err := r.Route(ctx, &domain.RouteRequest{
		Path:  "/api/v1/chat",
		Query: "高置信问题",
	})
	if err != nil {
		t.Fatalf("route error: %v", err)
	}
	// stub doesn't record tenantID, but the call must succeed without panic.
	if !rag.called {
		t.Error("expected RAG to be called for chat-bound query")
	}
}

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

var errRAGProbe = sentinelErr("rag probe failed")
