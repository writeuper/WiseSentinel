package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestOpsEvidenceProjectionSuppressesFreeText(t *testing.T) {
	const canary = "WS_OPS_HTTP_EVIDENCE_CANARY_1234567890"
	projected := toOpsEvidence([]domain.Evidence{{
		ToolName: "search_logs", Input: `{"query":"` + canary + `"}`,
		Output: `{"message":"` + canary + `"}`, Status: "success", LatencyMS: 17,
	}})
	if len(projected) != 1 {
		t.Fatalf("projection length = %d, want 1", len(projected))
	}
	if !projected[0].Suppressed || projected[0].InputBytes == 0 || projected[0].OutputBytes == 0 || projected[0].Source != "logs" {
		t.Fatalf("projection did not preserve expected safe diagnostics: %#v", projected[0])
	}
	raw, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), canary) || strings.Contains(string(raw), `"input"`) || strings.Contains(string(raw), `"output"`) {
		t.Fatalf("ops evidence API projection leaked tool body: %s", raw)
	}

	details := toSafeDetails([]string{"tool returned " + canary})
	if len(details) != 1 || strings.Contains(details[0], canary) || !strings.Contains(details[0], `"suppressed"`) {
		t.Fatalf("ops detail API projection leaked body: %#v", details)
	}
}

func TestSafeOpsResultNeverReturnsFreeText(t *testing.T) {
	got := safeOpsResult(domain.OpsTaskSuccess, "有哪些 firing 告警")
	if strings.Contains(got, "CANARY") || !strings.Contains(got, "结构化结论") || !strings.Contains(got, "告警") {
		t.Fatalf("unexpected success presentation: %q", got)
	}
	if got := safeOpsResult(domain.OpsTaskSuccess, "网关接口大量限流"); !strings.Contains(got, "rate limit triggered") || !strings.Contains(got, "止血") {
		t.Fatalf("unsafe or incomplete limit projection: %q", got)
	}
	if got := safeOpsResult(domain.OpsTaskFailed, "系统有点慢帮我看看"); !strings.Contains(got, "信息不足") || !strings.Contains(got, "指标") {
		t.Fatalf("missing safe clarification: %q", got)
	}
}
