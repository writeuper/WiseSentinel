package repository

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// AgentTraceRepo persists one trace record for an Agent request.
type AgentTraceRepo struct{}

type AgentTrace struct {
	TraceID    string
	TenantID   string
	UserID     string
	AgentType  string
	SessionID  string
	TaskID     string
	Query      string
	Status     string
	LatencyMS  int64
	ErrorMsg   string
	StartedAt  time.Time
	FinishedAt *time.Time
}

func NewAgentTraceRepo() *AgentTraceRepo { return &AgentTraceRepo{} }

func (r *AgentTraceRepo) Start(ctx context.Context, trace *AgentTrace) error {
	_, err := g.DB().Insert(ctx, "ws_agent_trace", g.Map{
		"trace_id":   trace.TraceID,
		"tenant_id":  trace.TenantID,
		"user_id":    trace.UserID,
		"agent_type": trace.AgentType,
		"session_id": trace.SessionID,
		"task_id":    trace.TaskID,
		"query_text": trace.Query,
		"status":     "running",
		"started_at": trace.StartedAt,
	})
	return err
}

func (r *AgentTraceRepo) Finish(ctx context.Context, traceID, status, errorMsg string, latencyMS int64) error {
	now := time.Now()
	_, err := g.DB().Model("ws_agent_trace").Ctx(ctx).
		Where("trace_id", traceID).
		Data(g.Map{
			"status":      status,
			"error_msg":   errorMsg,
			"latency_ms":  latencyMS,
			"finished_at": now,
		}).Update()
	return err
}
