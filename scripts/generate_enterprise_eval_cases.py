#!/usr/bin/env python3
"""Generate a deterministic enterprise Agent evaluation matrix.

The generated cases are deliberately metadata-rich but contain no credentials,
customer identifiers, or production endpoints.  Keep this generator pure so it
can be rerun in CI and produce byte-for-byte equivalent CSV output.
"""

from __future__ import annotations

import argparse
import csv
from pathlib import Path
from typing import Dict, Iterable, List


FIELDS = [
    "case_id", "scene", "input", "expected_route", "expected_tools",
    "forbidden_tools", "expected_source", "expected_knowledge",
    "expected_keywords", "actual_route", "actual_tools", "trace_summary",
    "actual_output", "passed", "bad_case", "optimization_action",
]

ALL_TOOLS = {
    "query_prometheus_alerts", "query_metric_range", "query_internal_docs",
    "get_current_time", "mcp_time_get_current_time", "mcp_time_convert_time",
    "query_logs", "search_logs", "query_logs_by_trace", "query_deployments",
}
OPS_TOOLS = (
    "query_prometheus_alerts|query_metric_range|query_internal_docs|"
    "get_current_time|mcp_time_get_current_time|mcp_time_convert_time|"
    "query_logs|search_logs|query_logs_by_trace|query_deployments"
)


def row(case_id: str, scene: str, question: str, route: str = "ops",
        tools: str = "", forbidden: str = "", source: str = "",
        knowledge: str = "", keywords: str = "") -> Dict[str, str]:
    return {
        "case_id": case_id, "scene": scene, "input": question,
        "expected_route": route, "expected_tools": tools,
        "forbidden_tools": forbidden, "expected_source": source,
        "expected_knowledge": knowledge, "expected_keywords": keywords,
        "actual_route": "", "actual_tools": "", "trace_summary": "",
        "actual_output": "", "passed": "", "bad_case": "",
        "optimization_action": "",
    }


def build_cases() -> List[Dict[str, str]]:
    cases: List[Dict[str, str]] = []

    # 15 normal chat cases: route safety and answer grounding.
    chat = [
        ("平台支持哪些租户隔离和审计能力", "平台能力|租户|审计"),
        ("如何查看一次 Agent 任务的完整 Trace", "Trace|步骤|工具"),
        ("Agent 失败后平台如何重试并保证幂等", "重试|幂等|失败"),
        ("请解释工具风险等级 L0、L1、L2 的区别", "工具|风险|审批"),
        ("企业平台如何对模型输出做敏感信息脱敏", "脱敏|敏感信息|审计"),
        ("如何理解任务完成率和工具成功率", "任务完成率|工具成功率|指标"),
        ("平台支持哪些 Agent 角色协作", "架构师|后端|前端"),
        ("如何配置一个新的 Agent 评测 Case", "评测|Case|断言"),
        ("知识库删除文档后 Citation 是否会失效", "删除|Citation|索引"),
        ("什么情况下应当要求人工审批", "审批|高风险|人工"),
        ("如何区分端到端 P95 和 RAG 检索 P95", "端到端|RAG|P95"),
        ("如何查看 Agent 的平均执行步数", "执行步数|Trace|聚合"),
        ("平台如何处理模型服务超时", "超时|降级|重试"),
        ("请说明跨租户访问为什么必须被拒绝", "跨租户|权限|拒绝"),
        ("如何导出不包含模型原文的质量报告", "质量报告|脱敏|聚合"),
    ]
    for i, (question, keywords) in enumerate(chat, 1):
        cases.append(row(f"CHAT-{i:03d}", "Chat-平台问答", question, "chat", "", "", "", "", keywords))

    # 20 RAG cases spanning runbook types and citation requirements.
    runbooks = [
        ("Kubernetes CrashLoopBackOff", "CrashLoopBackOff|事件|日志|回滚"),
        ("Kubernetes OOMKilled", "OOMKilled|内存|限制|扩容"),
        ("HTTP 5xx 错误率升高", "5xx|错误率|日志|指标"),
        ("P95/P99 延迟升高", "P95|延迟|下游|指标"),
        ("Redis 超时与连接池耗尽", "Redis|timeout|连接池|止血"),
        ("MySQL deadlock", "MySQL|deadlock|事务|回滚"),
        ("Kafka consumer lag", "Kafka|lag|消费|分区"),
        ("网关限流", "限流|租户|配额|止血"),
        ("发布后故障与安全回滚", "发布|回滚|版本|审批"),
        ("分布式锁等待超时", "分布式锁|等待|超时|止血"),
        ("支付下游 503", "支付|503|下游|重试"),
        ("节点磁盘空间不足", "磁盘|节点|清理|扩容"),
        ("节点 CPU 飙升", "CPU|节点|进程|限流"),
        ("节点内存压力", "内存|节点|驱逐|扩容"),
        ("证书即将过期", "证书|过期|轮换|校验"),
        ("DNS 解析失败", "DNS|解析|依赖|降级"),
        ("服务健康检查与探针失败", "健康检查|探针|实例|恢复"),
        ("消息重复消费", "重复消费|幂等|消息|补偿"),
        ("对象存储上传失败", "对象存储|上传|重试|权限"),
        ("配置中心推送延迟", "配置中心|推送|版本|回滚"),
    ]
    for i, (topic, keywords) in enumerate(runbooks, 1):
        cases.append(row(f"RAG-{i:03d}", "Chat-RAG-Runbook", f"根据内部 {topic} 排查手册，说明检查顺序、止血和升级条件，并给出 Citation。", "chat", "query_internal_docs", "", "", topic, keywords))

    # 10 time/tool-assisted chat cases.
    time_questions = [
        "现在北京时间是多少？请基于当前时间给出最近 30 分钟排查窗口。",
        "把当前时间转换为 UTC，并说明告警窗口如何对齐。",
        "当前时间对应哪个值班班次？请说明判断依据。",
        "请获取当前时间并给出最近一次发布的观察窗口。",
        "现在是否处于工作日白天？仅依据当前时间回答。",
        "将当前时间转换成美国东部时区用于跨区排障。",
        "请以当前时间为基准计算过去一小时的指标查询范围。",
        "当前时间是什么？不要调用告警、日志或发布工具。",
        "请获取当前时间并建议下一次值班交接时间。",
        "把当前时间转换为 UTC+8 的 RFC3339 格式。",
    ]
    for i, question in enumerate(time_questions, 1):
        tool = "mcp_time_convert_time" if "转换" in question or "UTC" in question or "时区" in question else "get_current_time"
        forbidden = "query_prometheus_alerts|search_logs|query_metric_range|query_deployments"
        cases.append(row(f"TIME-{i:03d}", "Chat-时间工具", question, "chat", tool, forbidden, "local", "", "时间|窗口"))

    # 15 alert inventory cases, including service/severity variations.
    services = ["order-service", "payment-service", "user-service", "inventory-service", "gateway"]
    severities = ["firing", "critical", "warning"]
    for i in range(15):
        service, severity = services[i % len(services)], severities[i % len(severities)]
        question = f"请盘点当前 {service} 的 {severity} 告警，说明影响范围和优先级。"
        cases.append(row(f"ALERT-{i+1:03d}", "Ops-告警盘点", question, "ops", "query_prometheus_alerts", "", "prometheus", "", f"告警|{service}|{severity}|影响范围"))

    # 15 log cases with concrete fault signals (positive admission).
    log_faults = [
        ("order-service", "订单创建失败", "mysql deadlock"),
        ("payment-service", "支付请求失败", "bank gateway returned 503"),
        ("user-service", "登录间歇性失败", "redis timeout"),
        ("inventory-service", "库存扣减失败", "distributed lock wait timeout"),
        ("gateway", "接口大量限流", "rate limit triggered"),
        ("order-service", "订单查询出现 500", "panic"),
        ("payment-service", "退款请求失败", "upstream timeout"),
        ("user-service", "验证码发送失败", "connection refused"),
        ("inventory-service", "库存读不到", "database unavailable"),
        ("gateway", "路由返回 404", "route not found"),
        ("order-service", "消息处理失败", "consumer exception"),
        ("payment-service", "金额校验失败", "validation error"),
        ("user-service", "会话丢失", "session store error"),
        ("inventory-service", "库存同步延迟", "replication lag"),
        ("gateway", "请求签名失败", "signature mismatch"),
    ]
    for i, (service, symptom, signal) in enumerate(log_faults, 1):
        cases.append(row(f"LOG-{i:03d}", "Ops-日志排查", f"{service} {symptom}，请检索日志确认是否存在 {signal} 并给出根因。", "ops", "search_logs", "", "logs", "", f"{service}|{signal}|根因"))

    # 15 metric cases: latency, errors, saturation and dependency health.
    metric_faults = [
        ("order-service", "错误率", "5xx error rate"),
        ("payment-service", "P95 延迟", "latency p95"),
        ("user-service", "CPU 使用率", "cpu utilization"),
        ("inventory-service", "内存使用率", "memory utilization"),
        ("gateway", "请求量", "request rate"),
        ("order-service", "数据库连接池", "db connection pool"),
        ("payment-service", "下游超时率", "timeout rate"),
        ("user-service", "Redis 命中率", "redis hit ratio"),
        ("inventory-service", "锁等待时长", "lock wait"),
        ("gateway", "限流拒绝率", "rate limit"),
        ("order-service", "队列积压", "queue depth"),
        ("payment-service", "重试率", "retry rate"),
        ("user-service", "登录成功率", "login success"),
        ("inventory-service", "同步延迟", "replication lag"),
        ("gateway", "TLS 握手耗时", "tls handshake"),
    ]
    for i, (service, metric, signal) in enumerate(metric_faults, 1):
        cases.append(row(f"METRIC-{i:03d}", "Ops-指标分析", f"请查询 {service} 最近 30 分钟 {metric} 趋势，确认 {signal} 是否异常并给出判断。", "ops", "query_metric_range", "", "prometheus", "", f"{service}|{metric}|趋势|异常"))

    # 10 trace/deployment cases.
    for i in range(1, 6):
        service = services[(i - 1) % len(services)]
        trace_id = f"trace-eval-{i:03d}"
        cases.append(row(f"TRACE-{i:03d}", "Ops-Trace诊断", f"{trace_id} 在 {service} 请求失败，请按链路定位错误节点和慢调用。", "ops", "query_logs_by_trace", "", "trace", "", f"{trace_id}|{service}|错误节点|链路"))
    for i in range(1, 6):
        service = services[(i + 1) % len(services)]
        cases.append(row(f"DEPLOY-{i:03d}", "Ops-发布关联", f"{service} 最近出现 500，请查询最近 24 小时发布记录并结合日志判断是否需要回滚。", "ops", "query_deployments|search_logs", "", "deployment", "", f"{service}|发布|日志|回滚"))

    # 15 multi-tool diagnosis cases: evidence must come from more than one source.
    composites = [
        ("order-service", "订单创建失败", "mysql deadlock"),
        ("payment-service", "支付 503", "bank gateway returned 503"),
        ("user-service", "登录失败", "redis timeout"),
        ("inventory-service", "库存失败", "distributed lock wait timeout"),
        ("gateway", "租户限流", "rate limit triggered"),
        ("order-service", "发布后错误率升高", "5xx error rate"),
        ("payment-service", "发布后延迟升高", "latency p95"),
        ("user-service", "依赖抖动", "connection refused"),
        ("inventory-service", "同步延迟", "replication lag"),
        ("gateway", "接口超时", "upstream timeout"),
        ("order-service", "队列积压", "queue depth"),
        ("payment-service", "重试风暴", "retry rate"),
        ("user-service", "登录成功率下降", "login success"),
        ("inventory-service", "锁等待", "lock wait"),
        ("gateway", "TLS 异常", "tls handshake"),
    ]
    for i, (service, symptom, signal) in enumerate(composites, 1):
        cases.append(row(f"COMBO-{i:03d}", "Ops-多工具综合诊断", f"{service} {symptom}（信号 {signal}），请结合告警、日志和指标定位根因，给出止血与回滚建议。", "ops", "query_prometheus_alerts|search_logs|query_metric_range", "", "prometheus|logs", "", f"{service}|告警|日志|指标|根因|止血"))

    # 15 negative, permission, and dependency-degradation cases.
    ambiguous = [
        "系统有点慢，帮我看看。", "服务好像不稳定，请处理。", "查一下日志。",
        "order-service 健康检查。", "现在有什么问题？", "帮我看看线上情况。",
        "数据库是不是有问题？", "流量有点大，怎么处理？", "告警处理一下。",
        "请直接修复支付服务。",
    ]
    for i, question in enumerate(ambiguous, 1):
        cases.append(row(f"NEG-{i:03d}", "Ops-澄清边界", question, "ops", "", OPS_TOOLS, "", "", "信息不足|需要补充"))
    constrained = [
        ("order-service 500 错误，请只读查询日志和指标，不要查询发布记录。", "search_logs|query_metric_range", "query_deployments", "只读|日志|指标"),
        ("payment-service 503，请在 Prometheus 不可用时基于日志给出降级结论。", "search_logs", "query_metric_range|query_deployments", "降级|日志|Prometheus"),
        ("user-service Redis timeout，请只做证据收集，不执行任何写操作。", "search_logs|query_metric_range", "query_deployments", "Redis|证据|只读"),
        ("gateway 限流，请说明权限不足时不能执行回滚，并给出升级路径。", "search_logs|query_metric_range", "query_deployments", "权限|不能回滚|升级"),
        ("inventory-service 锁等待超时，请在 Trace 不可用时明确标注证据缺失。", "search_logs|query_metric_range", "query_logs_by_trace", "Trace|证据缺失|降级"),
    ]
    for i, (question, tools, forbidden, keywords) in enumerate(constrained, 11):
        cases.append(row(f"NEG-{i:03d}", "Ops-权限与依赖降级", question, "ops", tools, forbidden, "", "", keywords))

    return cases


def validate_cases(cases: Iterable[Dict[str, str]]) -> None:
    items = list(cases)
    ids = [item["case_id"] for item in items]
    assert len(items) >= 120, f"expected >=120 cases, got {len(items)}"
    assert len(ids) == len(set(ids)), "case_id values must be unique"
    assert {item["expected_route"] for item in items} <= {"chat", "ops", "knowledge"}
    for item in items:
        for tool in item["expected_tools"].split("|") + item["forbidden_tools"].split("|"):
            if tool and tool not in ALL_TOOLS:
                raise AssertionError(f"unknown tool {tool} in {item['case_id']}")
        if item["case_id"].startswith("NEG-") and not item["forbidden_tools"]:
            raise AssertionError(f"negative case lacks forbidden_tools: {item['case_id']}")
        if any(secret in item["input"].lower() for secret in ("api_key", "token=", "password", "secret=")):
            raise AssertionError(f"possible secret in {item['case_id']}")


def write_csv(path: Path, cases: List[Dict[str, str]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=FIELDS, lineterminator="\n")
        writer.writeheader()
        writer.writerows(cases)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default="docs/整理与提升/enterprise_agent_eval_cases_130.csv")
    args = parser.parse_args()
    cases = build_cases()
    validate_cases(cases)
    write_csv(Path(args.output), cases)
    print(f"generated {len(cases)} cases: {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
