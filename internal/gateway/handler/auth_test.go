package handler

import (
	"context"
	"testing"

	v1 "wisesentinel-platform/api/v1"
)

func TestAuthTokenIsDisabledInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	controller := NewV1(nil)
	if _, err := controller.AuthToken(context.Background(), &v1.AuthTokenReq{
		Username: "sre@example.com",
		Password: "irrelevant",
	}); err == nil {
		t.Fatal("development token endpoint must be disabled in production")
	}
}
