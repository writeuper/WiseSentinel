package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wisesentinel-platform/internal/pkg/apperr"

	"github.com/cloudwego/eino/schema"
)

func TestRuntimeForSharesBudgetOnlyWithinSameDeploymentCredentialScope(t *testing.T) {
	router := &Router{runtimes: make(map[string]*modelRuntimeState), globalMaxConcurrent: 3}
	first := &ProfileConfig{Provider: "openai_compatible", Model: "model-a", APIKey: "credential-a", BaseURL: "https://example.invalid/v1", MaxConcurrent: 1}
	second := &ProfileConfig{Provider: "openai_compatible", Model: "model-a", APIKey: "credential-a", BaseURL: "https://example.invalid/v1", MaxConcurrent: 9}
	otherCredential := &ProfileConfig{Provider: "openai_compatible", Model: "model-a", APIKey: "credential-b", BaseURL: "https://example.invalid/v1", MaxConcurrent: 9}

	firstRuntime := router.runtimeFor(first)
	if got := router.runtimeFor(second); got != firstRuntime {
		t.Fatal("same deployment profiles did not share runtime state")
	}
	if got := router.runtimeFor(otherCredential); got == firstRuntime {
		t.Fatal("different credential scope shared runtime state")
	}
	if cap(firstRuntime.admission) != 3 {
		t.Fatalf("global admission cap = %d, want 3", cap(firstRuntime.admission))
	}
}

func TestSharedProfileRuntimeEnforcesOneBudgetAcrossModelInstances(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	router := &Router{runtimes: make(map[string]*modelRuntimeState), globalMaxConcurrent: 1}
	chatProfile := &ProfileConfig{Provider: "openai_compatible", Model: "model-a", APIKey: "credential", BaseURL: server.URL, Timeout: time.Second}
	opsProfile := &ProfileConfig{Provider: "openai_compatible", Model: "model-a", APIKey: "credential", BaseURL: server.URL, Timeout: time.Second}
	chat := newOpenAIEinoModel(chatProfile.Provider, chatProfile.Model, chatProfile.APIKey, chatProfile.BaseURL, chatProfile.Timeout, router.runtimeFor(chatProfile))
	ops := newOpenAIEinoModel(opsProfile.Provider, opsProfile.Model, opsProfile.APIKey, opsProfile.BaseURL, opsProfile.Timeout, router.runtimeFor(opsProfile))

	firstDone := make(chan error, 1)
	go func() {
		_, err := chat.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "chat"}})
		firstDone <- err
	}()
	<-started
	_, err := ops.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "ops"}})
	if !errors.Is(err, apperr.ErrModelOverloaded) {
		t.Fatalf("ops Generate error = %v, want shared-budget overload", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("chat Generate: %v", err)
	}
}
