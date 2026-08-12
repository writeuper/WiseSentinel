package repository

import (
	"context"
	"time"

	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/frame/g"
)

type ToolCallRecord struct {
	TenantID  string    `json:"tenant_id" orm:"tenant_id"`
	TraceID   string    `json:"trace_id" orm:"trace_id"`
	ToolName  string    `json:"tool_name" orm:"tool_name"`
	AgentType string    `json:"agent_type" orm:"agent_type"`
	Input     string    `json:"input_json" orm:"input_json"`
	Output    string    `json:"output_text" orm:"output_text"`
	Status    string    `json:"status" orm:"status"`
	LatencyMS int64     `json:"latency_ms" orm:"latency_ms"`
	CreatedAt time.Time `json:"created_at" orm:"created_at"`
}

type ToolCallRecordRepo struct{}

func NewToolCallRecordRepo() *ToolCallRecordRepo { return &ToolCallRecordRepo{} }

func (r *ToolCallRecordRepo) Create(ctx context.Context, rec *ToolCallRecord) error {
	_, err := g.DB().Insert(ctx, "ws_tool_call_record", g.Map{
		"tenant_id":   rec.TenantID,
		"trace_id":    rec.TraceID,
		"tool_name":   rec.ToolName,
		"agent_type":  rec.AgentType,
		"input_json":  redact.TelemetryProjection(rec.Input),
		"output_text": redact.TelemetryProjection(rec.Output),
		"status":      rec.Status,
		"latency_ms":  rec.LatencyMS,
	})
	return err
}
