package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/pkg/apperr"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestGenerateUsesOneTotalTimeoutBudgetAcrossRetries(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"late"}}]}`))
	}))
	defer server.Close()

	m := NewOpenAIEinoModel("test", "test-model", "key", server.URL, 20*time.Millisecond)
	started := time.Now()
	_, err := m.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "hello"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Generate error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("Generate took %s; timeout budget must cover retries", elapsed)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 after parent deadline", got)
	}
}

func TestGeneratePreservesUsageForTokenObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`))
	}))
	defer server.Close()

	m := NewOpenAIEinoModel("test", "test-model", "key", server.URL, time.Second)
	msg, err := m.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "hello"}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil || msg.ResponseMeta.Usage.TotalTokens != 19 {
		t.Fatalf("usage not preserved: %#v", msg.ResponseMeta)
	}
	families, err := observability.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ws_model_tokens_total" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["provider"] == "other" && labels["operation"] == "generate" && labels["token_type"] == "total" && metric.Counter.GetValue() >= 19 {
				return
			}
		}
	}
	t.Fatal("model token usage metric not observed")
}

func TestModelCallOutcomeUsesStableCategories(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "success"},
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "canceled"},
		{apperr.ErrModelOverloaded, "overloaded"},
		{errors.New("provider echoed token=do-not-label"), "upstream_error"},
	} {
		if got := modelCallOutcome(tc.err); got != tc.want {
			t.Fatalf("modelCallOutcome(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestWithToolsSharesNonBlockingAdmissionState(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	base := NewOpenAIEinoModelWithAdmission("test", "test-model", "key", server.URL, time.Second, 1)
	bound, err := base.WithTools([]*schema.ToolInfo{{Name: "safe_tool"}})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, callErr := base.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "one"}})
		firstDone <- callErr
	}()
	<-started
	_, err = bound.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "two"}})
	if !errors.Is(err, apperr.ErrModelOverloaded) {
		t.Fatalf("second Generate error = %v, want model overloaded", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Generate: %v", err)
	}
}

func TestCircuitBreakerAllowsOnlyOneHalfOpenProbe(t *testing.T) {
	m := NewOpenAIEinoModel("test", "test-model", "key", "http://localhost", time.Second)
	state := m.runtimeState()
	state.mu.Lock()
	state.breakerTripped = true
	state.failureCount = breakerThreshold
	state.lastFailureAt = time.Now().Add(-breakerResetTime - time.Second)
	state.mu.Unlock()
	if err := m.checkBreaker(); err != nil {
		t.Fatalf("first half-open probe rejected: %v", err)
	}
	if err := m.checkBreaker(); err == nil || !strings.Contains(err.Error(), "probe already in flight") {
		t.Fatalf("second half-open probe error = %v", err)
	}
	m.recordSuccess()
	if err := m.checkBreaker(); err != nil {
		t.Fatalf("breaker did not close after successful probe: %v", err)
	}
}

func TestCircuitBreakerEventsFollowStateTransitions(t *testing.T) {
	m := NewOpenAIEinoModel("test", "test-model", "key", "http://localhost", time.Second)
	for i := 0; i < breakerThreshold+2; i++ {
		m.recordFailure()
	}
	state := m.runtimeState()
	state.mu.RLock()
	tripped, failures := state.breakerTripped, state.failureCount
	state.mu.RUnlock()
	if !tripped || failures != breakerThreshold+2 {
		t.Fatalf("breaker state after repeated failures = tripped:%t failures:%d", tripped, failures)
	}
	m.recordSuccess()
	state.mu.RLock()
	tripped, failures = state.breakerTripped, state.failureCount
	state.mu.RUnlock()
	if tripped || failures != 0 {
		t.Fatalf("breaker state after recovery = tripped:%t failures:%d", tripped, failures)
	}
}

func TestStreamUsesProfileTimeoutForCompleteBody(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"first\"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	m := NewOpenAIEinoModel("test", "test-model", "key", server.URL, 40*time.Millisecond)
	reader, err := m.Stream(context.Background(), []*schema.Message{{Role: schema.User, Content: "hello"}})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer reader.Close()
	<-started
	for {
		_, err = reader.Recv()
		if err != nil {
			break
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stream Recv error = %v, want profile deadline", err)
	}
}

func TestBuildRequestNormalizesForcedToolChoice(t *testing.T) {
	m := NewOpenAIEinoModel("test", "test-model", "key", "http://localhost/v1", time.Second)
	tools := []*schema.ToolInfo{{Name: "search_logs", Desc: "search logs"}}
	m.tools = tools
	options := model.GetCommonOptions(nil, model.WithToolChoice(schema.ToolChoice("forced")))

	req := m.buildRequest(nil, options, false)
	if got := req["tool_choice"]; got != "required" {
		t.Fatalf("tool_choice = %v, want required", got)
	}
	if _, ok := req["tools"]; !ok {
		t.Fatal("request is missing tools")
	}
}

func TestBuildRequestOmitsToolChoiceWithoutTools(t *testing.T) {
	m := NewOpenAIEinoModel("test", "test-model", "key", "http://localhost/v1", time.Second)
	options := model.GetCommonOptions(nil, model.WithToolChoice(schema.ToolChoice("forced")))

	req := m.buildRequest(nil, options, false)
	if _, ok := req["tool_choice"]; ok {
		t.Fatalf("tool_choice = %v, want omitted when no tools are bound", req["tool_choice"])
	}
}

func TestBuildRequestDisablesThinkingForRequiredToolChoiceProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		baseURL  string
	}{
		{name: "ark base url", provider: "openai_compatible", model: "deepseek-v3", baseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		{name: "deepseek model", provider: "openai_compatible", model: "deepseek-v3", baseURL: "https://example.com/api/v3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewOpenAIEinoModel(tc.provider, tc.model, "key", tc.baseURL, time.Second)
			m.tools = []*schema.ToolInfo{{Name: "search_logs", Desc: "search logs"}}
			options := model.GetCommonOptions(nil, model.WithToolChoice(schema.ToolChoice("forced")))

			req := m.buildRequest(nil, options, false)
			if got := req["tool_choice"]; got != "required" {
				t.Fatalf("tool_choice = %v, want required", got)
			}
			thinking, ok := req["thinking"].(map[string]string)
			if !ok || thinking["type"] != "disabled" {
				t.Fatalf("thinking = %v, want disabled", req["thinking"])
			}
		})
	}
}
