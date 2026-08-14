package v1

import "github.com/gogf/gf/v2/frame/g"

// GetTraceReq queries one Agent trace with all recorded steps.
type GetTraceReq struct {
	g.Meta  `path:"/traces/{trace_id}" method:"get" tags:"Trace" summary:"查询 Agent Trace"`
	TraceID string `json:"trace_id" in:"path" v:"required"`
}

// GetTraceRes returns the main trace and its step timeline.
type GetTraceRes struct {
	Trace *AgentTrace      `json:"trace"`
	Steps []AgentTraceStep `json:"steps"`
}

type AgentTrace struct {
	TraceID       string `json:"trace_id"`
	TenantID      string `json:"tenant_id"`
	UserID        string `json:"user_id"`
	AgentType     string `json:"agent_type"`
	ConfigVersion string `json:"config_version,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	Query         string `json:"query"`
	Status        string `json:"status"`
	LatencyMS     int64  `json:"latency_ms"`
	ErrorMsg      string `json:"error_msg,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

type AgentTraceStep struct {
	ID             int64    `json:"id"`
	StepType       string   `json:"step_type"`
	StepName       string   `json:"step_name"`
	InputSummary   string   `json:"input_summary,omitempty"`
	OutputSummary  string   `json:"output_summary,omitempty"`
	EvidenceDocIDs []string `json:"evidence_doc_ids,omitempty"`
	Status         string   `json:"status"`
	LatencyMS      int64    `json:"latency_ms"`
	ErrorMsg       string   `json:"error_msg,omitempty"`
	CreatedAt      string   `json:"created_at,omitempty"`
}
