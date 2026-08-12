//go:build integration

package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestAgentTracePersistenceSuppressesTelemetryBodiesIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID, traceID := "trace-telemetry-"+uuid.NewString(), "trace-"+uuid.NewString()
	const canary = "WS_TRACE_TELEMETRY_CANARY_1234567890"
	repo := NewAgentTraceRepo()
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_agent_trace_step").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_agent_trace").Where("tenant_id", tenantID).Delete()
	})
	if missing, err := repo.Get(ctx, tenantID, "missing-"+uuid.NewString()); err != nil || missing != nil {
		t.Fatalf("missing trace = %#v, %v; want nil, nil", missing, err)
	}
	if err := repo.Start(ctx, &AgentTrace{TraceID: traceID, TenantID: tenantID, UserID: "tester", AgentType: "chat", Query: `{"content":"` + canary + `"}`, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddStep(ctx, &AgentTraceStep{TraceID: traceID, TenantID: tenantID, AgentType: "chat", StepType: "rag", StepName: "retrieve", InputSummary: canary, OutputSummary: `{"document":"` + canary + `"}`, ErrorMsg: canary}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, traceID, "failed", canary, 1); err != nil {
		t.Fatal(err)
	}
	var values []struct {
		QueryText string `json:"query_text"`
		ErrorMsg  string `json:"error_msg"`
	}
	if err := g.DB().Ctx(ctx).Model("ws_agent_trace").Where("tenant_id", tenantID).Fields("query_text,error_msg").Scan(&values); err != nil {
		t.Fatal(err)
	}
	var steps []struct {
		Input  string `json:"input_summary"`
		Output string `json:"output_summary"`
		Error  string `json:"error_msg"`
	}
	if err := g.DB().Ctx(ctx).Model("ws_agent_trace_step").Where("tenant_id", tenantID).Fields("input_summary,output_summary,error_msg").Scan(&steps); err != nil {
		t.Fatal(err)
	}
	for _, value := range append([]string{values[0].QueryText, values[0].ErrorMsg}, steps[0].Input, steps[0].Output, steps[0].Error) {
		if strings.Contains(value, canary) || !strings.Contains(value, `"suppressed":true`) {
			t.Fatalf("unsafe trace telemetry value: %q", value)
		}
	}
}

func TestOptionalTelemetryProjectionPreservesEmptyFields(t *testing.T) {
	if got := optionalTelemetryProjection(""); got != "" {
		t.Fatalf("empty projection = %q, want empty", got)
	}
	const canary = "WS_TRACE_OPTIONAL_CANARY_1234567890"
	if got := optionalTelemetryProjection(canary); got == "" || strings.Contains(got, canary) {
		t.Fatalf("non-empty projection must be present and redacted: %q", got)
	}
}
