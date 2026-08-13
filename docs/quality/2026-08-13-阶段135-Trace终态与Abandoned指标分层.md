# 阶段 135：Trace 终态与 Abandoned 指标分层

## 背景

启动恢复机制会把超过 10 分钟仍为 `running` 的历史 Trace 标记为 `abandoned`。如果质量报告把它与业务失败合并，会把进程崩溃/取消恢复问题误报为 Agent 业务失败，导致失败率和平均失败耗时失真。

## 变更

质量聚合报告现在区分：

- `success/completed`：业务成功；
- `failed/error/timeout/canceled` 等非成功业务终态：业务终态失败；
- `abandoned`：进程恢复/可观测性终态，单独计数和计算耗时；
- `running`：未完成，不进入 finished 分母。

报告新增：

- `abandoned` 数量；
- Abandoned 平均耗时；
- 业务终态平均耗时（排除 abandoned）；
- 全部 finished 平均耗时（包含 abandoned，保持总体运行观测）。

## 当前真实数据

- Finished Trace：1,945；
- Success/Completed：1,824；
- Abandoned：17；
- 业务终态平均耗时：7,398.68ms；
- 全部 finished 平均耗时：7,334.01ms；
- 平均 Trace 步数：2.928；
- 当前超过 10 分钟的 `running` Trace：0。

## 结论

业务失败率不再被历史 stale Trace 回收数量污染；同时保留 abandoned 指标用于监控进程崩溃、请求取消和服务重启恢复质量。生产建议对 `abandoned` 单独设置告警和 SLO，而不是并入 Agent 业务失败率。

## 关联代码

- [scripts/report_agent_quality_metrics.py](../../scripts/report_agent_quality_metrics.py)
- [internal/repository/agent_trace.go](../../internal/repository/agent_trace.go)
- [阶段134-取消路径审计完整性修复](./2026-08-13-阶段134-取消路径审计完整性修复.md)
