package mcp

import (
	"context"
	"reflect"
	"testing"
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
