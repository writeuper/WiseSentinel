package v1

import "github.com/gogf/gf/v2/frame/g"

// OpsAnalyzeReq triggers alert analysis.
type OpsAnalyzeReq struct {
	g.Meta  `path:"/ops/analyze" method:"post" tags:"Ops" summary:"告警分析"`
	Query   string             `json:"query"`
	Options *OpsAnalyzeOptions `json:"options"`
}

// OpsAnalyzeOptions configures ops analysis.
type OpsAnalyzeOptions struct {
	Async         bool `json:"async"`
	MaxIterations int  `json:"max_iterations" d:"20"`
}

// OpsAnalyzeRes returns analysis result or task id.
type OpsAnalyzeRes struct {
	TaskID     string              `json:"task_id"`
	Status     string              `json:"status"`
	Result     string              `json:"result,omitempty"`
	Detail     []string            `json:"detail,omitempty"`
	TraceID    string              `json:"trace_id"`
	Evidence   []OpsEvidence       `json:"evidence,omitempty"`
	Conclusion *OpsFaultConclusion `json:"conclusion,omitempty"`
	Timing     *OpsTiming          `json:"timing,omitempty"`
}

// OpsEvidence is one tool call performed during an Ops run.
type OpsEvidence struct {
	ToolName    string `json:"tool_name"`
	Source      string `json:"source,omitempty"`
	Status      string `json:"status"`
	LatencyMS   int64  `json:"latency_ms,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	Suppressed  bool   `json:"suppressed"`
	InputBytes  int    `json:"input_bytes,omitempty"`
	OutputBytes int    `json:"output_bytes,omitempty"`
}

// OpsTiming captures queue, execution and end-to-end troubleshooting latency.
type OpsTiming struct {
	QueueDurationMS int64  `json:"queue_duration_ms"`
	RunDurationMS   int64  `json:"run_duration_ms"`
	E2EDurationMS   int64  `json:"e2e_duration_ms"`
	CreatedAt       string `json:"created_at,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	FinishedAt      string `json:"finished_at,omitempty"`
}

// OpsFaultConclusion is the structured conclusion of an Ops troubleshooting run.
type OpsFaultConclusion struct {
	Symptom     string `json:"symptom"`
	Impact      string `json:"impact"`
	RootCause   string `json:"root_cause"`
	Workaround  string `json:"workaround"`
	Remediation string `json:"remediation"`
	Confidence  string `json:"confidence"`
	Source      string `json:"source"`
}

// GetOpsTaskReq queries an ops task.
type GetOpsTaskReq struct {
	g.Meta `path:"/ops/tasks/{task_id}" method:"get" tags:"Ops" summary:"查询 Ops 任务"`
	TaskID string `json:"task_id" in:"path" v:"required"`
}

// GetOpsTaskRes returns ops task status.
type GetOpsTaskRes struct {
	TaskID     string              `json:"task_id"`
	Status     string              `json:"status"`
	Result     string              `json:"result,omitempty"`
	Detail     []string            `json:"detail,omitempty"`
	Evidence   []OpsEvidence       `json:"evidence,omitempty"`
	Conclusion *OpsFaultConclusion `json:"conclusion,omitempty"`
	Timing     *OpsTiming          `json:"timing,omitempty"`
}

type CancelOpsTaskReq struct {
	g.Meta `path:"/ops/tasks/{task_id}/cancel" method:"post" tags:"Ops" summary:"取消 Ops 任务"`
	TaskID string `json:"task_id" in:"path" v:"required"`
}
type CancelOpsTaskRes struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// AlertWebhookReq receives Alertmanager webhook events.
type AlertWebhookReq struct {
	g.Meta            `path:"/webhook/alerts" method:"post" tags:"Ops" summary:"告警 Webhook 接入"`
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	GroupKey          string            `json:"groupKey"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Version           string            `json:"version"`
	Alerts            []AlertEvent      `json:"alerts"`
}

// AlertmanagerWebhookReq is the signed internal Alertmanager endpoint payload.
type AlertmanagerWebhookReq struct {
	g.Meta            `path:"/alertmanager" method:"post" tags:"Ops" summary:"Alertmanager 签名 Webhook"`
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	GroupKey          string            `json:"groupKey"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Version           string            `json:"version"`
	Alerts            []AlertEvent      `json:"alerts"`
}

// AlertEvent is a simplified alert payload.
type AlertEvent struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
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
