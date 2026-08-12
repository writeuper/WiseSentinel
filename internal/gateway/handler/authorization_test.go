package handler

import (
	"context"
	"testing"

	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestCanReadOwnedResource(t *testing.T) {
	owner := ctxkeys.WithUserID(context.Background(), "owner")
	if !canReadOwnedResource(owner, "owner") {
		t.Fatal("owner must read own resource")
	}
	if canReadOwnedResource(owner, "other") {
		t.Fatal("same-tenant non-owner must not read resource")
	}
	if canReadOwnedResource(owner, "") {
		t.Fatal("empty owner must not become public")
	}
}

func TestCanReadOwnedResourceAllowsOnlyAdministratorsAcrossUsers(t *testing.T) {
	operator := ctxkeys.WithRoles(ctxkeys.WithUserID(context.Background(), "operator"), []string{"operator"})
	if canReadOwnedResource(operator, "owner") {
		t.Fatal("operator must not receive cross-user access")
	}
	admin := ctxkeys.WithRoles(ctxkeys.WithUserID(context.Background(), "admin"), []string{"sre_admin"})
	if !canReadOwnedResource(admin, "owner") {
		t.Fatal("sre_admin must read tenant resources for operations")
	}
}
