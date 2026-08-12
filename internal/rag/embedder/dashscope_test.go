package embedder

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateEmbeddingRejectsNonFiniteValues(t *testing.T) {
	tests := []struct {
		name      string
		embedding []float32
	}{
		{name: "NaN", embedding: []float32{1, float32(math.NaN())}},
		{name: "positive infinity", embedding: []float32{1, float32(math.Inf(1))}},
		{name: "negative infinity", embedding: []float32{1, float32(math.Inf(-1))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateEmbedding(0, tt.embedding, 2); err == nil {
				t.Fatal("expected non-finite embedding to be rejected")
			}
		})
	}
}

func testEmbedderServer(t *testing.T, response string) *DashScopeEmbedder {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return &DashScopeEmbedder{apiKey: "test", model: "test", dimensions: 2, endpoint: server.URL, client: server.Client()}
}

func TestDashScopeEmbedderDoesNotExposeFailureBody(t *testing.T) {
	const canary = "WS_CANARY_DASHSCOPE_FAILURE_BODY_123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "request-123")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(canary))
	}))
	defer server.Close()
	embedder := &DashScopeEmbedder{apiKey: "test", model: "test", dimensions: 2, endpoint: server.URL, client: server.Client()}
	_, err := embedder.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("error leaked provider body: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 502") || !strings.Contains(err.Error(), "request_id=request-123") {
		t.Fatalf("unexpected stable error: %v", err)
	}
}

func TestDashScopeEmbedderRejectsInvalidSuccessfulResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		texts    []string
		want     string
	}{
		{"invalid index", `{"data":[{"index":1,"embedding":[1,2]}]}`, []string{"hello"}, "invalid embedding index"},
		{"duplicate index", `{"data":[{"index":0,"embedding":[1,2]},{"index":0,"embedding":[3,4]}]}`, []string{"first", "second"}, "duplicate embedding index"},
		{"wrong dimensions", `{"data":[{"index":0,"embedding":[1]}]}`, []string{"hello"}, "embedding dimension"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := testEmbedderServer(t, tt.response)
			_, err := embedder.Embed(context.Background(), tt.texts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Embed error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDashScopeEmbedderAcceptsCompleteIndexedEmbeddings(t *testing.T) {
	embedder := testEmbedderServer(t, `{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}]}`)
	got, err := embedder.Embed(context.Background(), []string{"first", "second"})
	if err != nil || len(got) != 2 || got[0][0] != 1 || got[1][0] != 3 {
		t.Fatalf("Embed result = %#v, %v", got, err)
	}
}

func TestDashScopeEmbedderRejectsOversizedSuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()
	embedder := &DashScopeEmbedder{apiKey: "test", model: "test", dimensions: 2, endpoint: server.URL, maxResponseBytes: 4, client: server.Client()}
	_, err := embedder.Embed(context.Background(), []string{"hello"})
	if err == nil || !strings.Contains(err.Error(), "exceeds configured size limit") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestDashScopeEmbedderAcceptsSuccessfulResponseAtConfiguredLimit(t *testing.T) {
	const response = `{"data":[{"index":0,"embedding":[1,2]}]}`
	embedder := testEmbedderServer(t, response)
	embedder.maxResponseBytes = int64(len(response))
	got, err := embedder.Embed(context.Background(), []string{"hello"})
	if err != nil || len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("Embed result = %#v, %v", got, err)
	}
}
