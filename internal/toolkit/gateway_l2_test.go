package toolkit

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestL2ToolFailsClosedWithoutDurableWorkflow(t *testing.T) {
	var invoked atomic.Int32
	gw := &Gateway{
		tools: map[string]*domain.ToolMeta{
			"write_change": {Name: "write_change", Enabled: true, RiskLevel: domain.ToolRiskL2Write, TimeoutMS: 1000},
		},
		adapters: map[string]AdapterFunc{
			"write_change": func(context.Context, json.RawMessage) (string, error) {
				invoked.Add(1)
				return "side effect", nil
			},
		},
	}
	for _, role := range []string{string(domain.RoleOperator), string(domain.RoleSREAdmin), string(domain.RolePlatformAdmin)} {
		ctx := ctxkeys.WithRoles(ctxkeys.WithTenantID(context.Background(), "tenant-a"), []string{role})
		resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{TenantID: "tenant-a", ToolName: "write_change", Input: json.RawMessage(`{"target":"production"}`)})
		if err != apperr.ErrHighRiskWorkflowUnavailable || resp != nil {
			t.Fatalf("role %s: response=%#v error=%v, want nil and durable-workflow error", role, resp, err)
		}
	}
	if got := invoked.Load(); got != 0 {
		t.Fatalf("L2 adapter invoked %d times; it must not run without a durable intent", got)
	}
}
