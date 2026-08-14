package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/redact"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// OpenAIEinoModel implements model.ToolCallingChatModel and model.ChatModel
// by wrapping the standard OpenAI-compatible HTTP API.
//
// It includes per-instance circuit breaker and retry logic for LLM API calls.
type OpenAIEinoModel struct {
	mu       sync.RWMutex
	provider string
	model    string
	apiKey   string
	baseURL  string
	timeout  time.Duration
	tools    []*schema.ToolInfo
	state    *modelRuntimeState
}

// modelRuntimeState is shared by every bound copy of one model profile. Tool
// binding is request-specific, while provider health and admission must be
// profile-scoped; otherwise a freshly constructed model bypasses both.
type modelRuntimeState struct {
	mu             sync.RWMutex
	failureCount   int
	lastFailureAt  time.Time
	breakerTripped bool
	// breakerProbeInFlight fences the half-open transition to one probe. It
	// prevents concurrent callers from all resetting the breaker together.
	breakerProbeInFlight bool
	admission            chan struct{}
}

const (
	maxRetries       = 2                      // 最多重试 2 次
	retryBaseDelay   = 500 * time.Millisecond // 初始退避 500ms
	breakerThreshold = 5                      // 连续 5 次失败则熔断
	breakerResetTime = 30 * time.Second       // 30s 后尝试半开
)

// NewOpenAIEinoModel creates a new Eino-compatible chat model.
func NewOpenAIEinoModel(provider, modelName, apiKey, baseURL string, timeout time.Duration) *OpenAIEinoModel {
	return NewOpenAIEinoModelWithAdmission(provider, modelName, apiKey, baseURL, timeout, 0)
}

// NewOpenAIEinoModelWithAdmission creates a model with a non-blocking,
// bounded provider admission limit. A value <= 0 disables local admission.
func NewOpenAIEinoModelWithAdmission(provider, modelName, apiKey, baseURL string, timeout time.Duration, maxConcurrent int) *OpenAIEinoModel {
	return newOpenAIEinoModel(provider, modelName, apiKey, baseURL, timeout, newModelRuntimeState(maxConcurrent))
}

func newModelRuntimeState(maxConcurrent int) *modelRuntimeState {
	state := &modelRuntimeState{}
	if maxConcurrent > 0 {
		state.admission = make(chan struct{}, maxConcurrent)
	}
	return state
}

func newOpenAIEinoModel(provider, modelName, apiKey, baseURL string, timeout time.Duration, state *modelRuntimeState) *OpenAIEinoModel {
	baseURL = strings.TrimRight(baseURL, "/\"' ")
	if state == nil {
		state = newModelRuntimeState(0)
	}
	return &OpenAIEinoModel{
		provider: provider,
		model:    modelName,
		apiKey:   apiKey,
		baseURL:  baseURL,
		timeout:  timeout,
		state:    state,
	}
}

func (m *OpenAIEinoModel) acquire(ctx context.Context) (func(), error) {
	started := time.Now()
	if m.state == nil || m.state.admission == nil {
		observability.ObserveModelAdmissionWait(m.provider, "accepted", time.Since(started).Seconds())
		return func() {}, nil
	}
	select {
	case m.state.admission <- struct{}{}:
		observability.ObserveModelAdmissionWait(m.provider, "accepted", time.Since(started).Seconds())
		observeRelease := observability.ObserveModelAdmission(m.provider, true)
		return func() {
			<-m.state.admission
			observeRelease()
		}, nil
	case <-ctx.Done():
		observability.ObserveModelAdmissionWait(m.provider, "canceled", time.Since(started).Seconds())
		return nil, ctx.Err()
	default:
		observability.ObserveModelAdmissionWait(m.provider, "rejected", time.Since(started).Seconds())
		observability.ObserveModelAdmission(m.provider, false)
		return nil, apperr.ErrModelOverloaded
	}
}

func modelHTTPError(resp *http.Response) error {
	requestID := redact.Summary(resp.Header.Get("X-Request-ID"), 128)
	if requestID == "" {
		requestID = redact.Summary(resp.Header.Get("X-Request-Id"), 128)
	}
	if requestID == "" {
		return fmt.Errorf("LLM HTTP %d", resp.StatusCode)
	}
	return fmt.Errorf("LLM HTTP %d request_id=%s", resp.StatusCode, requestID)
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

// Generate sends a non-streaming chat completion request with retry and circuit breaker.
func (m *OpenAIEinoModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (result *schema.Message, retErr error) {
	release, err := m.acquire(ctx)
	if err != nil {
		observability.ObserveModelCall(m.provider, "generate", modelCallOutcome(err), 0)
		return nil, err
	}
	defer release()
	executionStarted := time.Now()
	defer func() {
		observability.ObserveModelCall(m.provider, "generate", modelCallOutcome(retErr), time.Since(executionStarted).Seconds())
	}()
	// The profile timeout is a budget for the whole logical generation,
	// including retries and backoff. Without this parent deadline every retry
	// could consume a full upstream timeout and let a user request outlive its
	// intended SLO by several multiples.
	if m.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}
	// Check circuit breaker
	if err := m.checkBreaker(); err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s
			delay := retryBaseDelay * (1 << (attempt - 1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		msg, err := m.doGenerate(ctx, input, opts...)
		if err == nil {
			if msg != nil && msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				usage := msg.ResponseMeta.Usage
				observability.ObserveModelTokens(m.provider, "generate", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
			}
			m.recordSuccess()
			return msg, nil
		}

		lastErr = err
		// Only retry on transient errors (5xx, network errors)
		if !isRetryableError(err) {
			break
		}
	}

	m.recordFailure()
	return nil, fmt.Errorf("LLM call failed after %d retries: %w", maxRetries, lastErr)
}

// doGenerate performs a single non-streaming chat completion request.
func (m *OpenAIEinoModel) doGenerate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
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
	if resp.StatusCode >= 400 {
		return nil, modelHTTPError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var chatResp struct {
		Choices []struct {
			Message struct {
				Role      string           `json:"role"`
				Content   string           `json:"content"`
				ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
			Index        int    `json:"index"`
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
	toolCallCount := 0
	for _, choice := range chatResp.Choices {
		toolCallCount += len(choice.Message.ToolCalls)
	}
	g.Log().Debugf(ctx, "[model-debug] response model=%s status=%d choices=%d tool_calls=%d", m.model, resp.StatusCode, len(chatResp.Choices), toolCallCount)
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
func (m *OpenAIEinoModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (result *schema.StreamReader[*schema.Message], retErr error) {
	streamCtx := ctx
	var cancel context.CancelFunc
	if m.timeout > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, m.timeout)
	}
	executionStarted := time.Now()
	defer func() {
		observability.ObserveModelCall(m.provider, "stream_handshake", modelCallOutcome(retErr), time.Since(executionStarted).Seconds())
	}()
	release, err := m.acquire(streamCtx)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	options := model.GetCommonOptions(nil, opts...)

	reqBody := m.buildRequest(input, options, true)
	body, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
		m.chatCompletionURL(), bytes.NewReader(body))
	if err != nil {
		release()
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	// streamCtx owns the complete profile budget (handshake plus body). A
	// separate five-minute client timeout would let streaming bypass the
	// profile SLO and exhaust an admission slot long after sync calls stop.
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		release()
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("LLM stream request failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		release()
		if cancel != nil {
			cancel()
		}
		return nil, modelHTTPError(resp)
	}

	sr, ws := schema.Pipe[*schema.Message](0)

	go func() {
		streamStarted := time.Now()
		streamErr := error(nil)
		defer func() {
			observability.ObserveModelCall(m.provider, "stream_complete", modelCallOutcome(streamErr), time.Since(streamStarted).Seconds())
		}()
		defer resp.Body.Close()
		defer release()
		if cancel != nil {
			defer cancel()
		}
		defer ws.Close()

		// Accumulator for streaming chunks
		fullContent := strings.Builder{}
		var toolCalls []schema.ToolCall
		var streamUsage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil && readErr != io.EOF {
				streamErr = fmt.Errorf("read stream response: %w", readErr)
				ws.Send(nil, streamErr)
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if readErr == io.EOF {
					break
				}
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				if readErr == io.EOF {
					break
				}
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
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Usage != nil {
				streamUsage = chunk.Usage
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
		if len(toolCalls) > 0 {
			ws.Send(&schema.Message{
				Role:      schema.Assistant,
				Content:   fullContent.String(),
				ToolCalls: toolCalls,
			}, nil)
		}
		if streamUsage != nil {
			observability.ObserveModelTokens(m.provider, "stream_complete", streamUsage.PromptTokens, streamUsage.CompletionTokens, streamUsage.TotalTokens)
		}
	}()

	return sr, nil
}

func modelCallOutcome(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, apperr.ErrModelOverloaded):
		return "overloaded"
	default:
		return "upstream_error"
	}
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
		state:    m.state,
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
func isThinkingToolChoiceProvider(provider, baseURL, modelName string) bool {
	text := strings.ToLower(provider + " " + baseURL + " " + modelName)
	return strings.Contains(text, "ark") || strings.Contains(text, "volces") || strings.Contains(text, "deepseek")
}

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
		if opts.ToolChoice != nil && len(tools) > 0 {
			// Normalize tool_choice for OpenAI-compatible providers that do not
			// support Eino's "forced" value. "forced" semantically maps to
			// "required" (the model must call at least one tool). A required
			// choice without tools is invalid, so omit it when no tools are bound.
			choice := string(*opts.ToolChoice)
			if strings.EqualFold(choice, "forced") || strings.EqualFold(choice, "function") {
				choice = "required"
			}
			req["tool_choice"] = choice
			if strings.EqualFold(choice, "required") && isThinkingToolChoiceProvider(m.provider, m.baseURL, m.model) {
				req["thinking"] = map[string]string{"type": "disabled"}
			}
		}
	} else if len(m.tools) > 0 {
		req["tools"] = convertToolsToOpenAI(m.tools)
	}

	return req
}

// openAIToolCall is the OpenAI-compatible tool call structure.
type openAIToolCall struct {
	ID       string `json:"id,omitempty"`
	Index    *int   `json:"index,omitempty"`
	Type     string `json:"type"`
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
			schemaRef, err := t.ParamsOneOf.ToJSONSchema()
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

// ---------------------------------------------------------------------------
// Circuit breaker + retry helpers
// ---------------------------------------------------------------------------

// checkBreaker returns an error if the circuit breaker is tripped and hasn't
// reset yet. If the breaker has been tripped for longer than breakerResetTime,
// it allows a single probe request (half-open state).
func (m *OpenAIEinoModel) checkBreaker() error {
	state := m.runtimeState()
	state.mu.Lock()
	defer state.mu.Unlock()
	tripped := state.breakerTripped
	lastFailure := state.lastFailureAt

	if !tripped {
		return nil
	}

	// Half-open: allow a probe after the reset window
	if time.Since(lastFailure) > breakerResetTime {
		if state.breakerProbeInFlight {
			observability.ObserveModelBreakerEvent(m.provider, "rejected")
			return fmt.Errorf("LLM circuit breaker half-open probe already in flight")
		}
		state.breakerProbeInFlight = true
		observability.ObserveModelBreakerEvent(m.provider, "probe")
		return nil
	}

	observability.ObserveModelBreakerEvent(m.provider, "rejected")
	return fmt.Errorf("LLM circuit breaker is open (tripped after %d failures, retry in %v)",
		breakerThreshold, breakerResetTime-time.Since(lastFailure))
}

// recordSuccess resets the failure count on a successful call.
func (m *OpenAIEinoModel) recordSuccess() {
	state := m.runtimeState()
	state.mu.Lock()
	defer state.mu.Unlock()
	wasOpen := state.breakerTripped || state.breakerProbeInFlight
	state.failureCount = 0
	state.breakerTripped = false
	state.breakerProbeInFlight = false
	if wasOpen {
		observability.ObserveModelBreakerEvent(m.provider, "closed")
	}
}

// recordFailure increments the failure count and trips the breaker if
// the threshold is reached.
func (m *OpenAIEinoModel) recordFailure() {
	state := m.runtimeState()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.failureCount++
	state.lastFailureAt = time.Now()
	state.breakerProbeInFlight = false
	if state.failureCount >= breakerThreshold && !state.breakerTripped {
		state.breakerTripped = true
		observability.ObserveModelBreakerEvent(m.provider, "open")
	}
}

func (m *OpenAIEinoModel) runtimeState() *modelRuntimeState {
	if m.state == nil {
		// Legacy instances are only possible in package-local tests. Keep their
		// state usable rather than panicking; production constructors always set it.
		m.mu.Lock()
		if m.state == nil {
			m.state = &modelRuntimeState{}
		}
		m.mu.Unlock()
	}
	return m.state
}

// isRetryableError returns true if the error is likely transient
// (network error, 5xx, rate limit).
func isRetryableError(err error) bool {
	errStr := err.Error()
	// Network / timeout errors
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "reset by peer") {
		return true
	}
	// HTTP 5xx
	if strings.Contains(errStr, "HTTP 5") ||
		strings.Contains(errStr, "HTTP 429") {
		return true
	}
	return false
}

// Ensure OpenAIEinoModel implements model.ToolCallingChatModel.
var _ model.ToolCallingChatModel = (*OpenAIEinoModel)(nil)
var _ model.ChatModel = (*OpenAIEinoModel)(nil)
