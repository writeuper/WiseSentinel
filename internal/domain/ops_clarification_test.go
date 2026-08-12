package domain

import "testing"

func TestNeedsOpsClarificationDistinguishesVagueAndConcreteRequests(t *testing.T) {
	for _, query := range []string{
		"", "系统有点慢帮我看看", "系统很慢请帮忙看看",
		"订单服务", "订单服务健康检查", "查一下日志", "服务 down 了请先看告警再看相关日志",
	} {
		if !NeedsOpsClarification(query) {
			t.Fatalf("%q should require clarification", query)
		}
	}
	for _, query := range []string{
		"order-service 出现 500", "当前有哪些 firing 告警", "支付服务延迟升高", "trace-123 为什么失败",
		"order-service 最近 15 分钟错误率升高", "订单服务请查日志", "gateway 出现限流",
	} {
		if NeedsOpsClarification(query) {
			t.Fatalf("%q has concrete diagnostic evidence", query)
		}
	}
}
