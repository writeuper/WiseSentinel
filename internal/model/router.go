// Package model implements ModelRouter for LLM and embedding client resolution.
package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/configx"

	"github.com/gogf/gf/v2/frame/g"
)

// ChatModelClient is a simple OpenAI-compatible chat client for LLM calls.
type ChatModelClient struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
	Timeout  time.Duration
}

// ChatMessage represents a message in the chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the OpenAI-compatible request body.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// ToolCall represents a function call from the model.
type ToolCall struct {
	ID       string
	Type     string
	Function ToolCallFunction
}

// ToolCallFunction represents a function to be called.
type ToolCallFunction struct {
	Name      string
	Arguments string
}

// ChatCompletionResponse is the OpenAI-compatible response body.
type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ToolDefinition describes a tool for the LLM function calling API.
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a function in the LLM API.
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// Router implements domain.ModelRouter.
type Router struct {
	mu       sync.RWMutex
	profiles map[domain.ModelProfile]*ProfileConfig
}

// ProfileConfig stores resolved model profile configuration.
type ProfileConfig struct {
	Provider   string
	Model      string
	APIKey     string
	BaseURL    string
	Timeout    time.Duration
	Dimensions int
}

// NewRouter creates a model router from config.
func NewRouter(ctx context.Context) *Router {
	r := &Router{
		profiles: make(map[domain.ModelProfile]*ProfileConfig),
	}
	r.loadProfiles(ctx)
	return r
}

func (r *Router) loadProfiles(ctx context.Context) {
	profiles := map[domain.ModelProfile]string{
		domain.ModelProfileChatFast:         "models.profiles.chat_fast",
		domain.ModelProfileOpsPlan:          "models.profiles.ops_plan",
		domain.ModelProfileOpsExec:          "models.profiles.ops_exec",
		domain.ModelProfileEmbeddingDefault: "models.profiles.embedding_default",
	}
	for profile, path := range profiles {
		cfg := &ProfileConfig{
			Provider:   g.Cfg().MustGet(ctx, path+".provider").String(),
			Model:      configx.String(ctx, path+".model", modelEnvFor(profile)),
			APIKey:     configx.String(ctx, path+".api_key", apiKeyEnvFor(profile)),
			BaseURL:    configx.String(ctx, path+".base_url", baseURLEnvFor(profile)),
			Timeout:    time.Duration(g.Cfg().MustGet(ctx, path+".timeout_ms", 120000).Int()) * time.Millisecond,
			Dimensions: g.Cfg().MustGet(ctx, path+".dimensions", 2048).Int(),
		}
		if cfg.Timeout <= 0 {
			cfg.Timeout = 120 * time.Second
		}
		r.profiles[profile] = cfg
	}
}

func apiKeyEnvFor(profile domain.ModelProfile) string {
	switch profile {
	case domain.ModelProfileChatFast, domain.ModelProfileOpsPlan, domain.ModelProfileOpsExec:
		return "LLM_API_KEY"
	case domain.ModelProfileEmbeddingDefault:
		return "EMBED_API_KEY"
	default:
		return ""
	}
}

func baseURLEnvFor(profile domain.ModelProfile) string {
	switch profile {
	case domain.ModelProfileChatFast, domain.ModelProfileOpsPlan, domain.ModelProfileOpsExec:
		return "LLM_BASE_URL"
	default:
		return ""
	}
}

func modelEnvFor(profile domain.ModelProfile) string {
	switch profile {
	case domain.ModelProfileChatFast, domain.ModelProfileOpsPlan, domain.ModelProfileOpsExec:
		return "LLM_MODEL"
	default:
		return ""
	}
}

// ChatModel returns an Eino-compatible ToolCallingChatModel for the given profile.
func (r *Router) ChatModel(ctx context.Context, profile domain.ModelProfile) (any, error) {
	r.mu.RLock()
	cfg, ok := r.profiles[profile]
	r.mu.RUnlock()
	if !ok || cfg.Provider == "" {
		// Reload profiles on miss
		r.loadProfiles(ctx)
		r.mu.RLock()
		cfg, ok = r.profiles[profile]
		r.mu.RUnlock()
		if !ok || cfg.Provider == "" {
			return nil, fmt.Errorf("model profile %q not configured", profile)
		}
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("model profile %q: API key is required", profile)
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("model profile %q: base URL is required", profile)
	}
	return NewOpenAIEinoModel(cfg.Provider, cfg.Model, cfg.APIKey, cfg.BaseURL, cfg.Timeout), nil
}

// Ensure Router implements domain.ModelRouter.
var _ domain.ModelRouter = (*Router)(nil)
