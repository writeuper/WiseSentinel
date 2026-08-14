package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/frame/g"
)

// AgentTraceRepo persists one trace record for an Agent request.
type AgentTraceRepo struct{}

type AgentTrace struct {
	TraceID       string     `json:"trace_id" orm:"trace_id"`
	TenantID      string     `json:"tenant_id" orm:"tenant_id"`
	UserID        string     `json:"user_id" orm:"user_id"`
	AgentType     string     `json:"agent_type" orm:"agent_type"`
	ConfigVersion string     `json:"config_version" orm:"config_version"`
	SessionID     string     `json:"session_id" orm:"session_id"`
	TaskID        string     `json:"task_id" orm:"task_id"`
	Query         string     `json:"query" orm:"query_text"`
	Status        string     `json:"status" orm:"status"`
	LatencyMS     int64      `json:"latency_ms" orm:"latency_ms"`
	ErrorMsg      string     `json:"error_msg" orm:"error_msg"`
	StartedAt     time.Time  `json:"started_at" orm:"started_at"`
	FinishedAt    *time.Time `json:"finished_at" orm:"finished_at"`
}

type AgentTraceStep struct {
	ID            int64  `json:"id" orm:"id"`
	TraceID       string `json:"trace_id" orm:"trace_id"`
	TenantID      string `json:"tenant_id" orm:"tenant_id"`
	AgentType     string `json:"agent_type" orm:"agent_type"`
	StepType      string `json:"step_type" orm:"step_type"`
	StepName      string `json:"step_name" orm:"step_name"`
	InputSummary  string `json:"input_summary" orm:"input_summary"`
	OutputSummary string `json:"output_summary" orm:"output_summary"`
	// EvidenceDocIDs is bounded structured metadata, kept separate from
	// redacted summaries so Citation grounding can be verified safely.
	EvidenceDocIDs []string  `json:"evidence_doc_ids,omitempty" orm:"-"`
	Status         string    `json:"status" orm:"status"`
	LatencyMS      int64     `json:"latency_ms" orm:"latency_ms"`
	ErrorMsg       string    `json:"error_msg" orm:"error_msg"`
	CreatedAt      time.Time `json:"created_at" orm:"created_at"`
}

// optionalTelemetryProjection preserves absence as absence. A synthetic
// projection for an empty error changes the semantic meaning of a successful
// trace step and corrupts error-rate and UI diagnostics.
func optionalTelemetryProjection(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return redact.TelemetryProjection(value)
}

func NewAgentTraceRepo() *AgentTraceRepo { return &AgentTraceRepo{} }

// ReapStaleRunning closes traces left behind by a process crash or a pre-fix
// canceled stream. Only traces older than the supplied age are touched, so a
// live request cannot be mistaken for an abandoned one.
func (r *AgentTraceRepo) ReapStaleRunning(ctx context.Context, age time.Duration) (int64, error) {
	if age <= 0 {
		age = 10 * time.Minute
	}
	cutoff := time.Now().Add(-age)
	result, err := g.DB().Model("ws_agent_trace").Ctx(ctx).
		Where("status", "running").
		WhereLT("started_at", cutoff).
		Data(g.Map{
			"status":      "abandoned",
			"error_msg":   optionalTelemetryProjection("trace abandoned after stale running timeout"),
			"finished_at": time.Now(),
		}).Update()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *AgentTraceRepo) Start(ctx context.Context, trace *AgentTrace) error {
	_, err := g.DB().InsertIgnore(ctx, "ws_agent_trace", g.Map{
		"trace_id":       trace.TraceID,
		"tenant_id":      trace.TenantID,
		"user_id":        trace.UserID,
		"agent_type":     trace.AgentType,
		"config_version": redact.Summary(trace.ConfigVersion, 64),
		"session_id":     trace.SessionID,
		"task_id":        trace.TaskID,
		"query_text":     redact.TelemetryProjection(trace.Query),
		"status":         "running",
		"started_at":     trace.StartedAt,
	})
	return err
}

func (r *AgentTraceRepo) Finish(ctx context.Context, traceID, status, errorMsg string, latencyMS int64) error {
	now := time.Now()
	_, err := g.DB().Model("ws_agent_trace").Ctx(ctx).
		Where("trace_id", traceID).
		Data(g.Map{
			"status":      status,
			"error_msg":   optionalTelemetryProjection(errorMsg),
			"latency_ms":  latencyMS,
			"finished_at": now,
		}).Update()
	return err
}

func (r *AgentTraceRepo) AddStep(ctx context.Context, step *AgentTraceStep) error {
	var evidenceJSON any
	if ids := normalizeEvidenceDocIDs(step.EvidenceDocIDs); len(ids) > 0 {
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		evidenceJSON = string(encoded)
	}
	_, err := g.DB().Insert(ctx, "ws_agent_trace_step", g.Map{
		"trace_id":         step.TraceID,
		"tenant_id":        step.TenantID,
		"agent_type":       step.AgentType,
		"step_type":        step.StepType,
		"step_name":        step.StepName,
		"input_summary":    optionalTelemetryProjection(step.InputSummary),
		"output_summary":   optionalTelemetryProjection(step.OutputSummary),
		"evidence_doc_ids": evidenceJSON,
		"status":           step.Status,
		"latency_ms":       step.LatencyMS,
		"error_msg":        optionalTelemetryProjection(step.ErrorMsg),
	})
	return err
}

func (r *AgentTraceRepo) Get(ctx context.Context, tenantID, traceID string) (*AgentTrace, error) {
	var row AgentTrace
	err := g.DB().Model("ws_agent_trace").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("trace_id", traceID).
		Scan(&row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.TraceID == "" {
		return nil, nil
	}
	return &row, nil
}

func (r *AgentTraceRepo) ListSteps(ctx context.Context, tenantID, traceID string) ([]AgentTraceStep, error) {
	var rows []struct {
		AgentTraceStep
		EvidenceJSON string `json:"evidence_doc_ids" orm:"evidence_doc_ids"`
	}
	err := g.DB().Model("ws_agent_trace_step").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("trace_id", traceID).
		OrderAsc("id").
		Scan(&rows)
	if err != nil {
		return nil, err
	}
	steps := make([]AgentTraceStep, 0, len(rows))
	for _, row := range rows {
		step := row.AgentTraceStep
		if strings.TrimSpace(row.EvidenceJSON) != "" {
			_ = json.Unmarshal([]byte(row.EvidenceJSON), &step.EvidenceDocIDs)
			step.EvidenceDocIDs = normalizeEvidenceDocIDs(step.EvidenceDocIDs)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func normalizeEvidenceDocIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, minInt(len(ids), 32))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
		if len(result) == 32 {
			break
		}
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
