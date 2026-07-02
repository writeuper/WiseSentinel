package domain_test

import (
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestMaxSecretLevelForRoles(t *testing.T) {
	if got := domain.MaxSecretLevelForRoles([]string{"viewer"}); got != domain.SecretLevelInternal {
		t.Fatalf("viewer max = %d, want %d", got, domain.SecretLevelInternal)
	}
	if got := domain.MaxSecretLevelForRoles([]string{"sre_admin"}); got != domain.SecretLevelSensitive {
		t.Fatalf("sre_admin max = %d, want %d", got, domain.SecretLevelSensitive)
	}
}
