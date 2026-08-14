package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestValidateCommandRequiresAbsoluteAndAllowlistedPath(t *testing.T) {
	if err := validateCommand("uvx", nil); err == nil {
		t.Fatal("relative MCP command must be rejected")
	}
	if err := validateCommand("/opt/bin/mcp", []string{"/opt/bin/other"}); err == nil {
		t.Fatal("command outside allowlist must be rejected")
	}
	if err := validateCommand("/opt/bin/mcp", []string{"/opt/bin/mcp"}); err != nil {
		t.Fatalf("allowlisted absolute command rejected: %v", err)
	}
}

func TestValidateArgsRequiresExactConfiguredProfile(t *testing.T) {
	allowed := []string{"--with", "mcp==1.23.0", "mcp-server-time"}
	if err := validateArgs(allowed, allowed); err != nil {
		t.Fatalf("allowlisted arguments rejected: %v", err)
	}
	if err := validateArgs([]string{"--from", "evil", "mcp-server-time"}, allowed); err == nil {
		t.Fatal("unapproved arguments must be rejected")
	}
}

func TestSanitizedEnvironmentExcludesPlatformSecrets(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/mcp-home",
		"LLM_API_KEY=secret",
		"MYSQL_DSN=mysql-secret",
		"JWT_SECRET=jwt-secret",
	}
	got := sanitizedEnvironment(env, []string{"PATH", "HOME"})
	want := []string{"PATH=/usr/bin", "HOME=/tmp/mcp-home"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized environment = %#v, want %#v", got, want)
	}
}

func TestRestrictedCommandFactoryReplacesParentEnvironment(t *testing.T) {
	command := restrictedCommandFactory([]string{"PATH=/usr/bin", "TZ=UTC"})
	cmd, err := command(context.Background(), "/bin/echo", []string{"JWT_SECRET=leaked"}, []string{"ok"})
	if err != nil {
		t.Fatalf("create restricted command: %v", err)
	}
	if !reflect.DeepEqual(cmd.Env, []string{"PATH=/usr/bin", "TZ=UTC"}) {
		t.Fatalf("command environment = %#v", cmd.Env)
	}
}

func testCapabilityManifest(t *testing.T) CapabilityManifest {
	t.Helper()
	initialized := &mcp.InitializeResult{
		ProtocolVersion: "2025-06-18",
		ServerInfo:      mcp.Implementation{Name: "safe-tools", Version: "1.2.3"},
	}
	tools := &mcp.ListToolsResult{Tools: []mcp.Tool{
		{Name: "search", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]any{"query": map[string]any{"type": "string"}}}},
		{Name: "status", InputSchema: mcp.ToolInputSchema{Type: "object"}},
	}}
	manifest, err := buildCapabilityManifest(initialized, tools)
	if err != nil {
		t.Fatalf("buildCapabilityManifest: %v", err)
	}
	return manifest
}

func TestCapabilityManifestPinsIdentityToolsAndFingerprint(t *testing.T) {
	manifest := testCapabilityManifest(t)
	if manifest.ServerName != "safe-tools" || manifest.ServerVersion != "1.2.3" || manifest.ToolCount != 2 {
		t.Fatalf("unexpected capability manifest: %#v", manifest)
	}
	if len(manifest.SchemaFingerprint) != 64 {
		t.Fatalf("schema fingerprint length = %d, want SHA-256 hex", len(manifest.SchemaFingerprint))
	}
	policy := &CapabilityPolicy{
		ExpectedServerName:    "safe-tools",
		ExpectedServerVersion: "1.2.3",
		ExpectedToolNames:     []string{"status", "search"},
		ExpectedFingerprint:   manifest.SchemaFingerprint,
		MaxTools:              2,
	}
	if err := validateCapabilityManifest(manifest, policy); err != nil {
		t.Fatalf("valid capability policy rejected: %v", err)
	}
}

func TestCapabilityManifestFailsClosedOnDriftAndResourceExcess(t *testing.T) {
	manifest := testCapabilityManifest(t)
	cases := []struct {
		name   string
		policy CapabilityPolicy
	}{
		{name: "server version", policy: CapabilityPolicy{ExpectedServerVersion: "9.9.9"}},
		{name: "tool set", policy: CapabilityPolicy{ExpectedToolNames: []string{"search"}}},
		{name: "fingerprint", policy: CapabilityPolicy{ExpectedFingerprint: "deadbeef"}},
		{name: "tool limit", policy: CapabilityPolicy{MaxTools: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCapabilityManifest(manifest, &tc.policy); err == nil {
				t.Fatal("capability drift or excess was accepted")
			}
		})
	}
}

func TestToolSchemaFingerprintChangesWhenSchemaChanges(t *testing.T) {
	base := []mcp.Tool{{Name: "search", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]any{"query": map[string]any{"type": "string"}}}}}
	changed := []mcp.Tool{{Name: "search", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]any{"query": map[string]any{"type": "integer"}}}}}
	first, err := toolSchemaFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := toolSchemaFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("schema fingerprint did not change after schema drift")
	}
	if _, err := json.Marshal(base[0]); err != nil {
		t.Fatalf("tool schema should remain marshalable: %v", err)
	}
}

type pagedToolLister struct {
	pages  []*mcp.ListToolsResult
	called int
}

func (p *pagedToolLister) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	if p.called >= len(p.pages) {
		return nil, nil
	}
	result := p.pages[p.called]
	p.called++
	return result, nil
}

func TestListAllToolsConsumesPagesAndRejectsRepeatedCursor(t *testing.T) {
	lister := &pagedToolLister{pages: []*mcp.ListToolsResult{
		{Tools: []mcp.Tool{{Name: "first"}}, PaginatedResult: mcp.PaginatedResult{NextCursor: "next"}},
		{Tools: []mcp.Tool{{Name: "second"}}},
	}}
	result, err := listAllTools(context.Background(), lister)
	if err != nil {
		t.Fatalf("listAllTools: %v", err)
	}
	if result == nil || len(result.Tools) != 2 || lister.called != 2 {
		t.Fatalf("paged tools = %#v, calls=%d", result, lister.called)
	}

	repeating := &pagedToolLister{pages: []*mcp.ListToolsResult{
		{PaginatedResult: mcp.PaginatedResult{NextCursor: "same"}},
		{PaginatedResult: mcp.PaginatedResult{NextCursor: "same"}},
	}}
	if _, err := listAllTools(context.Background(), repeating); err == nil {
		t.Fatal("repeated pagination cursor must fail closed")
	}
}
