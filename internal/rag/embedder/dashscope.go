package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"wisesentinel-platform/internal/pkg/configx"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	dashScopeEmbedURL = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding"
	defaultModel      = "text-embedding-v4"
	defaultDimensions = 2048
)

// DashScopeEmbedder calls Alibaba DashScope text-embedding API.
type DashScopeEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

func NewDashScopeEmbedder(ctx context.Context) (*DashScopeEmbedder, error) {
	apiKey := configx.String(ctx, "models.profiles.embedding_default.api_key", "EMBED_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("EMBED_API_KEY is required for DashScope embedder")
	}
	model := g.Cfg().MustGet(ctx, "models.profiles.embedding_default.model", defaultModel).String()
	dim := g.Cfg().MustGet(ctx, "models.profiles.embedding_default.dimensions", defaultDimensions).Int()
	return &DashScopeEmbedder{
		apiKey:     apiKey,
		model:      model,
		dimensions: dim,
		client:     &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (e *DashScopeEmbedder) Dimensions() int {
	return e.dimensions
}

func (e *DashScopeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body := map[string]any{
		"model": e.model,
		"input": map[string]any{
			"texts": texts,
		},
		"parameters": map[string]any{
			"dimension": e.dimensions,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashScopeEmbedURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dashscope embed http %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Output struct {
			Embeddings []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"embeddings"`
		} `json:"output"`
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Output.Embeddings) != len(texts) {
		return nil, fmt.Errorf("dashscope returned %d embeddings for %d texts", len(parsed.Output.Embeddings), len(texts))
	}

	out := make([][]float32, len(texts))
	for i, item := range parsed.Output.Embeddings {
		out[i] = item.Embedding
	}
	return out, nil
}
