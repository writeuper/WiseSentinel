package ops

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestParseConclusion_FullSections(t *testing.T) {
	result := `经排查，order-service 错误率从 0.1% 升至 5.2%。
故障现象：下单接口 500 错误率突增
影响范围：移动端下单全量受影响，约 12% 用户无法完成支付
根因判断：payment-client v2.3.1 引入的空指针，在金额为 0 时触发
临时止血：回滚 payment-client 至 v2.3.0
根治建议：在 payment-client 增加 nil 校验并补单测，下周随 v2.3.2 上线
置信度：high`

	c := parseConclusion(result)
	if c == nil {
		t.Fatal("expected non-nil conclusion")
	}
	if c.Symptom != "下单接口 500 错误率突增" {
		t.Errorf("symptom = %q", c.Symptom)
	}
	if !strings.Contains(c.Impact, "12%") {
		t.Errorf("impact = %q", c.Impact)
	}
	if !strings.Contains(c.RootCause, "空指针") {
		t.Errorf("root_cause = %q", c.RootCause)
	}
	if !strings.Contains(c.Workaround, "回滚") {
		t.Errorf("workaround = %q", c.Workaround)
	}
	if !strings.Contains(c.Remediation, "nil 校验") {
		t.Errorf("remediation = %q", c.Remediation)
	}
	if c.Confidence != "high" {
		t.Errorf("confidence = %q, want high", c.Confidence)
	}
	if c.Source != "实时排查结论" {
		t.Errorf("source = %q, want 实时排查结论", c.Source)
	}
}

func TestParseConclusion_PartialSections(t *testing.T) {
	// Missing workaround / remediation should not break parsing.
	result := `故障现象：CPU 100%
根因判断：死循环`
	c := parseConclusion(result)
	if c == nil {
		t.Fatal("expected non-nil conclusion")
	}
	if c.Symptom != "CPU 100%" {
		t.Errorf("symptom = %q", c.Symptom)
	}
	if c.RootCause != "死循环" {
		t.Errorf("root_cause = %q", c.RootCause)
	}
	if c.Workaround != "" {
		t.Errorf("workaround should be empty, got %q", c.Workaround)
	}
	if c.Remediation != "" {
		t.Errorf("remediation should be empty, got %q", c.Remediation)
	}
	// Default confidence is mid when not specified.
	if c.Confidence != "mid" {
		t.Errorf("confidence = %q, want mid", c.Confidence)
	}
}

func TestParseConclusion_Empty(t *testing.T) {
	if c := parseConclusion(""); c != nil {
		t.Errorf("expected nil for empty result, got %+v", c)
	}
	if c := parseConclusion("   "); c != nil {
		t.Errorf("expected nil for whitespace result, got %+v", c)
	}
}

func TestMarshalUnmarshalPayload_RoundTrip(t *testing.T) {
	detail := []string{"[planner] step1", "[executor] done"}
	evidence := []domain.Evidence{
		{ToolName: "query_logs", Status: "success", LatencyMS: 120},
	}
	conclusion := &domain.FaultConclusion{
		Symptom:    "high error rate",
		RootCause:  "bad deploy",
		Confidence: "mid",
		Source:     "实时排查结论",
	}
	encoded := marshalPayload(detail, evidence, conclusion)
	p := unmarshalPayload(encoded)
	if len(p.Detail) != 2 || p.Detail[0] != detail[0] {
		t.Errorf("detail round-trip mismatch: %+v", p.Detail)
	}
	if len(p.Evidence) != 1 || p.Evidence[0].ToolName != "query_logs" {
		t.Errorf("evidence round-trip mismatch: %+v", p.Evidence)
	}
	if p.Conclusion == nil || p.Conclusion.Symptom != "high error rate" {
		t.Errorf("conclusion round-trip mismatch: %+v", p.Conclusion)
	}
}

func TestUnmarshalPayload_LegacyDetailArray(t *testing.T) {
	// Older rows stored detail_json as a bare JSON array of strings.
	legacy := `["[planner] step1","[executor] done"]`
	p := unmarshalPayload(legacy)
	if len(p.Detail) != 2 {
		t.Fatalf("legacy detail len = %d, want 2", len(p.Detail))
	}
	if p.Evidence != nil {
		t.Errorf("legacy evidence should be nil")
	}
	if p.Conclusion != nil {
		t.Errorf("legacy conclusion should be nil")
	}
}
