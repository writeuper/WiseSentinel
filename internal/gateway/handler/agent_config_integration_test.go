//go:build integration

package handler

import (
	"context"
	"os"
	"strings"
	"testing"

	v1 "wisesentinel-platform/api/v1"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

func TestActivateAgentConfigIsAtomicAndAuditableIntegration(t *testing.T) {
	dsn := os.Getenv("OPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPS_TEST_MYSQL_DSN is not configured")
	}
	gdb.SetConfigGroup("default", gdb.ConfigGroup{gdb.ConfigNode{Link: dsn}})
	ctx := context.Background()
	tenantID := "config-audit-itest-" + uuid.NewString()
	ctx = ctxkeys.WithTenantID(ctx, tenantID)
	ctx = ctxkeys.WithUserID(ctx, "admin")
	ctx = ctxkeys.WithTraceID(ctx, "trace-"+uuid.NewString())
	controller := NewV1(nil)
	validV1 := `{"system_prompt":"safe-v1","max_iterations":20,"tools":["query_logs"]}`
	invalidV2 := `{"system_prompt":"unsafe-v2","max_iterations":20,"tools":["query_logs","query_logs"]}`
	validV2 := `{"system_prompt":"safe-v2","max_iterations":10,"tools":["query_logs"]}`
	for _, row := range []g.Map{
		{"tenant_id": tenantID, "agent_type": "chat", "version": "v1", "config_json": validV1, "is_active": 1, "created_by": "test"},
		{"tenant_id": tenantID, "agent_type": "chat", "version": "v2", "config_json": invalidV2, "is_active": 0, "created_by": "test"},
	} {
		if _, err := g.DB().Ctx(ctx).Model("ws_agent_config").Data(row).Insert(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = g.DB().Ctx(ctx).Model("ws_audit_log").Where("tenant_id", tenantID).Delete()
		_, _ = g.DB().Ctx(ctx).Model("ws_agent_config").Where("tenant_id", tenantID).Delete()
	})
	invalidReq := structActivateReq("chat", "v2")
	if _, err := controller.ActivateAgentConfig(ctx, &invalidReq); err == nil {
		t.Fatal("invalid config activated")
	}
	var active struct{ Version string }
	if err := g.DB().Ctx(ctx).Model("ws_agent_config").Where("tenant_id", tenantID).Where("is_active", 1).Scan(&active); err != nil || active.Version != "v1" {
		t.Fatalf("active version after rejection = %#v, %v", active, err)
	}
	if _, err := g.DB().Ctx(ctx).Model("ws_agent_config").Where("tenant_id", tenantID).Where("version", "v2").Data(g.Map{"config_json": validV2}).Update(); err != nil {
		t.Fatal(err)
	}
	validReq := structActivateReq("chat", "v2")
	res, err := controller.ActivateAgentConfig(ctx, &validReq)
	if err != nil || res.PreviousVersion != "v1" || len(res.ConfigSHA256) != 64 {
		t.Fatalf("activation response = %#v, %v", res, err)
	}
	var audit struct{ RequestJSON string }
	if err := g.DB().Ctx(ctx).Model("ws_audit_log").Where("tenant_id", tenantID).Where("action", "agent_config.activate").Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit.RequestJSON, `"previous_version":"v1"`) || !strings.Contains(audit.RequestJSON, `"new_version":"v2"`) || strings.Contains(audit.RequestJSON, "safe-v2") {
		t.Fatalf("audit payload = %s", audit.RequestJSON)
	}
}

// Keep the integration request construction independent from generated API
// validation details while reusing the public request type.
func structActivateReq(agentType, version string) v1.ActivateAgentConfigReq {
	return v1.ActivateAgentConfigReq{AgentType: agentType, Version: version}
}
