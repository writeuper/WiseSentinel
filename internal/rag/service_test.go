package rag

import (
	"context"
	"errors"
	"testing"

	"wisesentinel-platform/internal/domain"
)

type recordingRetriever struct {
	requests  []*domain.RetrieveRequest
	responses map[string]*domain.RetrieveResponse
	errors    map[string]error
}

func (r *recordingRetriever) Retrieve(_ context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	copy := *req
	r.requests = append(r.requests, &copy)
	if err := r.errors[req.Query]; err != nil {
		return nil, err
	}
	return r.responses[req.Query], nil
}

func TestRetrieveMultiFusesVariantsAndPreservesFilters(t *testing.T) {
	backend := &recordingRetriever{responses: map[string]*domain.RetrieveResponse{
		"服务下线告警怎么处理？":     {Documents: []domain.RetrievedDocument{{ChunkID: "a", DocID: "d1", Score: .6}, {ChunkID: "b", DocID: "d2", Score: .5}}},
		"服务下线告警排查步骤 处置流程": {Documents: []domain.RetrievedDocument{{ChunkID: "a", DocID: "d1", Score: .7}, {ChunkID: "c", DocID: "d3", Score: .65}}},
	}}
	svc := &Service{retriever: backend}
	resp, err := svc.Retrieve(context.Background(), &domain.RetrieveRequest{TenantID: "tenant-a", Query: "服务下线告警怎么处理？", TopK: 2, MaxSecretLevel: domain.SecretLevelInternal, EnableQueryExpansion: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.QueryCount != 2 || !resp.Expanded || len(resp.Documents) != 2 || resp.Documents[0].ChunkID != "a" || resp.Documents[0].Score != .7 || resp.Documents[1].ChunkID != "c" {
		t.Fatalf("unexpected fused response: %#v", resp)
	}
	for _, request := range backend.requests {
		if request.TenantID != "tenant-a" || request.MaxSecretLevel != domain.SecretLevelInternal || request.EnableQueryExpansion || len(request.QueryVariants) != 0 {
			t.Fatalf("variant lost security boundary or recursively expanded: %#v", request)
		}
	}
}

func TestRetrieveMultiFailsClosedWhenOriginalQueryFails(t *testing.T) {
	backend := &recordingRetriever{responses: map[string]*domain.RetrieveResponse{}, errors: map[string]error{"original": errors.New("dependency unavailable")}}
	svc := &Service{retriever: backend}
	if _, err := svc.Retrieve(context.Background(), &domain.RetrieveRequest{Query: "original", QueryVariants: []string{"variant"}}); err == nil {
		t.Fatal("original-query failure must fail the whole retrieval")
	}
}
