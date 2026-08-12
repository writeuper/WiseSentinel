package toolkit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestGatewayUsesRedactedObservabilityProjection(t *testing.T) {
	const canary = "WS_CANARY_SECRET_1234567890"
	gw := &Gateway{
		tools: map[string]*domain.ToolMeta{
			"safe_test": {Name: "safe_test", Enabled: true, RiskLevel: domain.ToolRiskL0Readonly},
		},
		adapters: map[string]AdapterFunc{},
	}
	gw.adapters["safe_test"] = func(_ context.Context, input json.RawMessage) (string, error) {
		if !strings.Contains(string(input), canary) {
			t.Fatalf("adapter must receive original input, got %s", input)
		}
		return `{"authorization":"Bearer ` + canary + `","detail":"ok"}`, nil
	}

	var evidence []domain.Evidence
	var stepInput, stepOutput string
	ctx := ctxkeys.WithTenantID(context.Background(), "tenant-a")
	ctx = ctxkeys.WithToolSink(ctx, &evidence)
	ctx = ctxkeys.WithStepSink(ctx, func(_ string, _ string, input, output, _ string, _ int64, _ string) {
		stepInput, stepOutput = input, output
	})

	resp, err := gw.Invoke(ctx, &domain.ToolInvokeRequest{
		TenantID: "tenant-a", ToolName: "safe_test",
		Input: json.RawMessage(`{"token":"` + canary + `","query":"healthy"}`),
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if !strings.Contains(resp.Output, canary) {
		t.Fatalf("business response should preserve adapter output: %s", resp.Output)
	}
	for _, value := range []string{evidence[0].Input, evidence[0].Output, stepInput, stepOutput} {
		if strings.Contains(value, canary) {
			t.Fatalf("observability projection leaked canary: %s", value)
		}
		if !strings.Contains(value, `"suppressed":true`) {
			t.Fatalf("observability projection must be fail-closed: %s", value)
		}
	}
}
