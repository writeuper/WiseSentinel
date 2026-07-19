package domain_test

import (
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestAgentTypeConstants(t *testing.T) {
	tests := []struct {
		got  domain.AgentType
		want string
	}{
		{domain.AgentTypeChat, "chat"},
		{domain.AgentTypeOps, "ops"},
		{domain.AgentTypeKnowledge, "knowledge"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("AgentType = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestToolRiskLevelConstants(t *testing.T) {
	tests := []struct {
		got  domain.ToolRiskLevel
		want string
	}{
		{domain.ToolRiskL0Readonly, "L0_READONLY"},
		{domain.ToolRiskL1SensitiveRead, "L1_SENSITIVE_READ"},
		{domain.ToolRiskL2Write, "L2_WRITE"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("ToolRiskLevel = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestRoleConstants(t *testing.T) {
	tests := []struct {
		got  domain.Role
		want string
	}{
		{domain.RoleViewer, "viewer"},
		{domain.RoleOperator, "operator"},
		{domain.RoleSREAdmin, "sre_admin"},
		{domain.RolePlatformAdmin, "platform_admin"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("Role = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestDefaultTenantID(t *testing.T) {
	if domain.DefaultTenantID != "default" {
		t.Errorf("DefaultTenantID = %q, want %q", domain.DefaultTenantID, "default")
	}
}

func TestSecretLevelConstants(t *testing.T) {
	if domain.SecretLevelInternal != 1 {
		t.Errorf("SecretLevelInternal = %d, want 1", domain.SecretLevelInternal)
	}
	if domain.SecretLevelSensitive != 2 {
		t.Errorf("SecretLevelSensitive = %d, want 2", domain.SecretLevelSensitive)
	}
}