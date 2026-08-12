# Kubernetes OOMKilled 排查 Runbook

## 适用范围

适用于容器因内存超限被终止（`OOMKilled`）的场景。指标和命令仅为脱敏示例。

## 排查顺序

1. 确认容器终止原因、退出码及最近 1 小时重启趋势。
2. 对比工作负载内存 limit、request 与实际 RSS/working set 曲线。
3. 检查发布、流量、批任务、缓存大小和大对象请求是否在告警前变化。
4. 区分容器达到 limit、节点 MemoryPressure、JVM/Go runtime 内存异常。
5. 关联接口 P95、并发数、队列长度和下游超时，确认业务影响。

示例 PromQL：`container_memory_working_set_bytes{pod=~"<workload>-.*"}`、`kube_pod_container_resource_limits`。

## 处置

短期按审批临时扩容或降低并发，避免无限制提高 limit；暂停高风险批任务。若为泄漏，保留 heap/profile 摘要并回滚问题版本。恢复后观察内存斜率、重启次数、错误率至少一个业务峰值周期。

## 升级条件

连续两个周期 OOM、节点出现 MemoryPressure、多个租户受影响或疑似敏感数据进入 dump 时，立即升级并限制诊断输出。

## 证据

终止原因、资源配置、内存曲线、流量曲线、版本与变更记录、影响接口、扩容/回滚审批记录。
