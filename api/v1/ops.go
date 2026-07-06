package v1

import "github.com/gogf/gf/v2/frame/g"

// OpsAnalyzeReq triggers alert analysis.
type OpsAnalyzeReq struct {
	g.Meta  `path:"/ops/analyze" method:"post" tags:"Ops" summary:"告警分析"`
	Query   string            `json:"query"`
	Options *OpsAnalyzeOptions `json:"options"`
}

// OpsAnalyzeOptions configures ops analysis.
type OpsAnalyzeOptions struct {
	Async         bool `json:"async"`
	MaxIterations int  `json:"max_iterations" d:"20"`
}

// OpsAnalyzeRes returns analysis result or task id.
type OpsAnalyzeRes struct {
	TaskID  string   `json:"task_id"`
	Status  string   `json:"status"`
	Result  string   `json:"result,omitempty"`
	Detail  []string `json:"detail,omitempty"`
	TraceID string   `json:"trace_id"`
}

// GetOpsTaskReq queries an ops task.
type GetOpsTaskReq struct {
	g.Meta `path:"/ops/tasks/{task_id}" method:"get" tags:"Ops" summary:"查询 Ops 任务"`
	TaskID string `json:"task_id" in:"path" v:"required"`
}

// GetOpsTaskRes returns ops task status.
type GetOpsTaskRes struct {
	TaskID string   `json:"task_id"`
	Status string   `json:"status"`
	Result string   `json:"result,omitempty"`
	Detail []string `json:"detail,omitempty"`
}

// AlertWebhookReq receives Alertmanager webhook events.
type AlertWebhookReq struct {
	g.Meta `path:"/webhook/alerts" method:"post" tags:"Ops" summary:"告警 Webhook 接入"`
	Status string       `json:"status"`
	Alerts []AlertEvent `json:"alerts"`
}

// AlertEvent is a simplified alert payload.
type AlertEvent struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
}

// AlertWebhookRes confirms webhook receipt.
type AlertWebhookRes struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// ListOpsTasksReq lists recent ops tasks for the current tenant (M5).
type ListOpsTasksReq struct {
	g.Meta `path:"/ops/tasks" method:"get" tags:"Ops" summary:"Ops 任务列表"`
	Page   int    `json:"page" d:"1" in:"query"`
	Size   int    `json:"size" d:"20" in:"query"`
	Status string `json:"status" in:"query"`
}

// ListOpsTasksRes returns recent ops tasks.
type ListOpsTasksRes struct {
	Items []OpsTaskSummary `json:"items"`
	Total int              `json:"total"`
}

// OpsTaskSummary is a row in /ops/tasks listing.
type OpsTaskSummary struct {
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	TriggerType string `json:"trigger_type"`
	CreatedAt   string `json:"created_at"`
	CreatedBy   string `json:"created_by"`
}

// CurrentUserRes returns the identity decoded from the JWT (M5).
// The handler is registered under /api/v1/me.
type CurrentUserReq struct {
	g.Meta `path:"/me" method:"get" tags:"Auth" summary:"当前用户信息"`
}
type CurrentUserRes struct {
	Username string   `json:"username"`
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}
