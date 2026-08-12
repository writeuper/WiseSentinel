package domain

import (
	"regexp"
	"strings"
)

var (
	// A service identity is an operational target, not merely any arbitrary
	// noun in a natural-language question. Keep this recognition deliberately
	// narrow until service catalog resolution is available.
	serviceIDPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9-]*-service\b`)
	traceIDPattern   = regexp.MustCompile(`(?i)\btrace[-_][a-z0-9._-]+\b`)
)

// NeedsOpsClarification reports whether a request lacks the minimum scoped
// evidence required to read operational data. This is a positive admission
// policy, rather than a growing deny-list of vague words: a request needs a
// trace reference, an explicit global alert inventory, or both an operational
// target and a diagnostic signal. The rule is shared by the agent execution
// boundary and API presentation so a safe refusal cannot drift into an opaque
// task failure.
func NeedsOpsClarification(query string) bool {
	text := strings.ToLower(strings.TrimSpace(query))
	if text == "" {
		return true
	}
	if traceIDPattern.MatchString(text) {
		return false
	}
	// An explicitly current alert inventory is a bounded global read. It must
	// remain alert-only in focused tool selection; it does not authorize broad
	// log or metric discovery without a target.
	if isGlobalAlertInventory(text) {
		return false
	}
	return !hasOperationalTarget(text) || !hasDiagnosticSignal(text)
}

func hasOperationalTarget(text string) bool {
	if serviceIDPattern.MatchString(text) {
		return true
	}
	for _, target := range []string{"订单服务", "支付服务", "用户服务", "库存服务", "网关", "gateway"} {
		if strings.Contains(text, target) {
			return true
		}
	}
	return false
}

func hasDiagnosticSignal(text string) bool {
	for _, signal := range []string{
		"告警", "alert", "日志", "log", "指标", "metric", "错误率", "延迟", "latency",
		"500", "502", "503", "504", "timeout", "超时", "redis", "mysql", "发布", "部署", "回滚", "限流", "锁",
		"失败", "down", "deadlock", "firing",
	} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func isGlobalAlertInventory(text string) bool {
	hasAlert := strings.Contains(text, "告警") || strings.Contains(text, "alert") || strings.Contains(text, "firing")
	hasInventoryIntent := strings.Contains(text, "当前") || strings.Contains(text, "哪些") || strings.Contains(text, "列表") || strings.Contains(text, "查看")
	return hasAlert && hasInventoryIntent
}
