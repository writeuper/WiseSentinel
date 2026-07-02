package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// OpenAIEinoModel implements model.ToolCallingChatModel and model.ChatModel
// by wrapping the standard OpenAI-compatible HTTP API.
type OpenAIEinoModel struct {
	mu       sync.RWMutex
	provider string
	model    string
	apiKey   string
	baseURL  string
	timeout  time.Duration
	tools    []*schema.ToolInfo
}

// NewOpenAIEinoModel creates a new Eino-compatible chat model.
func NewOpenAIEinoModel(provider, modelName, apiKey, baseURL string, timeout time.Duration) *OpenAIEinoModel {
	baseURL = strings.TrimRight(baseURL, "/\"' ")
	return &OpenAIEinoModel{
		provider: provider,
		model:    modelName,
		apiKey:   apiKey,
		baseURL:  baseURL,
		timeout:  timeout,
	}
}

// chatCompletionURL returns the full URL for the chat completions endpoint.
// It detects whether the base URL already includes a version segment (e.g. /v1, /v3)
// and appends the correct path accordingly.
func (m *OpenAIEinoModel) chatCompletionURL() string {
	// If the base URL already contains a version-like segment, just append /chat/completions.
	// Otherwise, follow the standard OpenAI convention: {base}/v1/chat/completions.
	path := m.baseURL
	if hasAPIVersion(path) {
		return path + "/chat/completions"
	}
	return path + "/v1/chat/completions"
}

// hasAPIVersion checks if the URL path contains a version segment like /v1, /v2, /v3 etc.
func hasAPIVersion(u string) bool {
	// Find the last path segment
	idx := strings.LastIndex(u, "/")
	if idx < 0 {
		return false
	}
	seg := u[idx+1:]
	// Check if it matches /v{n}
	if len(seg) >= 2 && seg[0] == 'v' {
		for _, c := range seg[1:] {
			if c < '0' || c > '9' {
				return false
			}
		}
		return len(seg) > 1
	}
	return false
}

// Generate sends a non-streaming chat completion request.
func (m *OpenAIEinoModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	options := model.GetCommonOptions(nil, opts...)

	reqBody := m.buildRequest(input, options, false)
	body, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.chatCompletionURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)

	client := &http.Client{Timeout: m.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Role         string           `json:"role"`
				Content      string           `json:"content"`
				ToolCalls    []openAIToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string           `json:"finish_reason"`
			Index        int              `json:"index"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}

	choice := chatResp.Choices[0]
	msg := &schema.Message{
		Role:    schema.RoleType(choice.Message.Role),
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		msg.ToolCalls = make([]schema.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			msg.ToolCalls[i] = schema.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: schema.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	if chatResp.Usage != nil {
		msg.ResponseMeta = &schema.ResponseMeta{
			Usage: &schema.TokenUsage{
				PromptTokens:     chatResp.Usage.PromptTokens,
				CompletionTokens: chatResp.Usage.CompletionTokens,
				TotalTokens:      chatResp.Usage.TotalTokens,
			},
			FinishReason: choice.FinishReason,
		}
	}

	return msg, nil
}

// Stream sends a streaming chat completion request.
func (m *OpenAIEinoModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	options := model.GetCommonOptions(nil, opts...)

	reqBody := m.buildRequest(input, options, true)
	body, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.chatCompletionURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LLM stream request failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	sr, ws := schema.Pipe[*schema.Message](0)

	go func() {
		defer resp.Body.Close()
		defer ws.Close()

		// Accumulator for streaming chunks
		fullContent := strings.Builder{}
		var toolCalls []schema.ToolCall
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []openAIToolCall `json:"tool_calls,omitempty"` // deprecated in streaming, but some models still use
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					fullContent.WriteString(c.Delta.Content)
					ws.Send(&schema.Message{
						Role:    schema.RoleType(c.Delta.Role),
						Content: c.Delta.Content,
					}, nil)
				}
				// Handle streaming tool calls
				if len(c.Delta.ToolCalls) > 0 {
					for _, tc := range c.Delta.ToolCalls {
						tc := tc
						idx := 0
						if tc.Index != nil {
							idx = *tc.Index
						}
						for len(toolCalls) <= idx {
							toolCalls = append(toolCalls, schema.ToolCall{})
						}
						toolCalls[idx].ID = tc.ID
						toolCalls[idx].Type = tc.Type
						toolCalls[idx].Function.Name += tc.Function.Name
						toolCalls[idx].Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}
	}()

	return sr, nil
}

// WithTools returns a new OpenAIEinoModel with the given tools bound.
func (m *OpenAIEinoModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	newModel := &OpenAIEinoModel{
		provider: m.provider,
		model:    m.model,
		apiKey:   m.apiKey,
		baseURL:  m.baseURL,
		timeout:  m.timeout,
		tools:    tools,
	}
	return newModel, nil
}

// BindTools implements the deprecated ChatModel interface.
func (m *OpenAIEinoModel) BindTools(tools []*schema.ToolInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = tools
	return nil
}

// buildRequest constructs the OpenAI-compatible request body.
func (m *OpenAIEinoModel) buildRequest(input []*schema.Message, opts *model.Options, stream bool) map[string]interface{} {
	req := map[string]interface{}{
		"model":  m.model,
		"stream": stream,
	}

	// Convert messages
	msgs := make([]map[string]interface{}, 0, len(input))
	for _, msg := range input {
		m := map[string]interface{}{
			"role":    string(msg.Role),
			"content": msg.Content,
		}
		if len(msg.ToolCalls) > 0 {
			tcs := make([]map[string]interface{}, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				tcs[i] = map[string]interface{}{
					"id":   tc.ID,
					"type": tc.Type,
					"function": map[string]string{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				}
			}
			m["tool_calls"] = tcs
		}
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		msgs = append(msgs, m)
	}
	req["messages"] = msgs

	// Apply options
	if opts != nil {
		if opts.Temperature != nil {
			req["temperature"] = *opts.Temperature
		}
		if opts.MaxTokens != nil {
			req["max_tokens"] = *opts.MaxTokens
		}
		if opts.TopP != nil {
			req["top_p"] = *opts.TopP
		}
		if len(opts.Stop) > 0 {
			req["stop"] = opts.Stop
		}
		// Use tools from the model instance (set via WithTools)
		tools := m.tools
		if len(opts.Tools) > 0 {
			tools = opts.Tools
		}
		if len(tools) > 0 {
			req["tools"] = convertToolsToOpenAI(tools)
		}
		if opts.ToolChoice != nil {
			req["tool_choice"] = string(*opts.ToolChoice)
		}
	} else if len(m.tools) > 0 {
		req["tools"] = convertToolsToOpenAI(m.tools)
	}

	return req
}

// openAIToolCall is the OpenAI-compatible tool call structure.
type openAIToolCall struct {
	ID       string  `json:"id,omitempty"`
	Index    *int    `json:"index,omitempty"`
	Type     string  `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func convertToolsToOpenAI(tools []*schema.ToolInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		tool := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Desc,
			},
		}
		if t.ParamsOneOf != nil {
			schemaRef, err := t.ParamsOneOf.ToOpenAPIV3()
			if err == nil && schemaRef != nil {
				tool["function"].(map[string]interface{})["parameters"] = schemaRef
			}
		}
		// If no parameters, still provide an empty object
		if _, ok := tool["function"].(map[string]interface{})["parameters"]; !ok {
			tool["function"].(map[string]interface{})["parameters"] = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		result = append(result, tool)
	}
	return result
}

// Ensure OpenAIEinoModel implements model.ToolCallingChatModel.
var _ model.ToolCallingChatModel = (*OpenAIEinoModel)(nil)
var _ model.ChatModel = (*OpenAIEinoModel)(nil)