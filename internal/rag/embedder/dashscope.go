package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	dashScopeEmbedURL       = "https://llm-3fbwukq9239nrw0s.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/embeddings"
	defaultModel            = "qwen3.7-text-embedding"
	defaultDimensions       = 2048
	defaultMaxResponseBytes = 4 * 1024 * 1024
)

// DashScopeEmbedder calls Alibaba DashScope text-embedding API.
type DashScopeEmbedder struct {
	apiKey           string
	model            string
	dimensions       int
	endpoint         string
	maxResponseBytes int64
	client           *http.Client
}

func NewDashScopeEmbedder(ctx context.Context) (*DashScopeEmbedder, error) {
	apiKey := configx.String(ctx, "models.profiles.embedding_default.api_key", "EMBED_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("EMBED_API_KEY is required for DashScope embedder")
	}
	model := g.Cfg().MustGet(ctx, "models.profiles.embedding_default.model", defaultModel).String()
	dim := g.Cfg().MustGet(ctx, "models.profiles.embedding_default.dimensions", defaultDimensions).Int()
	maxResponseBytes := g.Cfg().MustGet(ctx, "models.profiles.embedding_default.max_response_bytes", defaultMaxResponseBytes).Int64()
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	return &DashScopeEmbedder{
		apiKey:           apiKey,
		model:            model,
		dimensions:       dim,
		endpoint:         dashScopeEmbedURL,
		maxResponseBytes: maxResponseBytes,
		client:           &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (e *DashScopeEmbedder) Dimensions() int {
	return e.dimensions
}

func (e *DashScopeEmbedder) Endpoint() string {
	if e.endpoint != "" {
		return e.endpoint
	}
	return dashScopeEmbedURL
}

func (e *DashScopeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// MaaS OpenAI-compatible API: input is a string or []string, not {"texts": [...]}.
	// Dimensions is a top-level field, not nested under "parameters".
	var input any = texts[0]
	if len(texts) > 1 {
		input = texts
	}
	body := map[string]any{
		"model":      e.model,
		"input":      input,
		"dimensions": e.dimensions,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	g.Log().Debugf(ctx, "DashScope embed request: endpoint=%s model=%s texts=%d dimensions=%d", e.Endpoint(), e.model, len(texts), e.dimensions)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, dashScopeHTTPError(resp)
	}

	raw, err := readEmbeddingResponse(resp.Body, e.responseLimit())
	if err != nil {
		return nil, err
	}
	// MaaS returns OpenAI-compatible format: {"data": [{"embedding": [...]}]}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("dashscope returned %d embeddings for %d texts", len(parsed.Data), len(texts))
	}

	out := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("dashscope returned invalid embedding index %d", item.Index)
		}
		if out[item.Index] != nil {
			return nil, fmt.Errorf("dashscope returned duplicate embedding index %d", item.Index)
		}
		if err := validateEmbedding(item.Index, item.Embedding, e.dimensions); err != nil {
			return nil, err
		}
		out[item.Index] = item.Embedding
	}
	for index, embedding := range out {
		if embedding == nil {
			return nil, fmt.Errorf("dashscope omitted embedding index %d", index)
		}
	}
	return out, nil
}

func validateEmbedding(index int, embedding []float32, dimensions int) error {
	if len(embedding) != dimensions {
		return fmt.Errorf("dashscope returned embedding dimension %d for index %d, want %d", len(embedding), index, dimensions)
	}
	for valueIndex, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("dashscope returned non-finite embedding value at index %d dimension %d", index, valueIndex)
		}
	}
	return nil
}

func (e *DashScopeEmbedder) responseLimit() int64 {
	if e.maxResponseBytes > 0 {
		return e.maxResponseBytes
	}
	return defaultMaxResponseBytes
}

func readEmbeddingResponse(body io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("dashscope embedding response exceeds configured size limit")
	}
	return raw, nil
}

func dashScopeHTTPError(resp *http.Response) error {
	requestID := redact.Summary(resp.Header.Get("X-Request-ID"), 128)
	if requestID == "" {
		return fmt.Errorf("dashscope embed HTTP %d", resp.StatusCode)
	}
	return fmt.Errorf("dashscope embed HTTP %d request_id=%s", resp.StatusCode, requestID)
}
