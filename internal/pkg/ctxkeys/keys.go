package ctxkeys

import (
	"context"

	"wisesentinel-platform/internal/domain"
)

type ctxKey string

const (
	TenantID     ctxKey = "tenant_id"
	UserID       ctxKey = "user_id"
	TraceID      ctxKey = "trace_id"
	Roles        ctxKey = "roles"
	SessionID    ctxKey = "session_id"
	ToolSink     ctxKey = "tool_sink" // *[]domain.Evidence, populated by Gateway.Invoke
	StepSink     ctxKey = "step_sink" // StepSinkFunc, populated by Agent runtime
	RequestQuery ctxKey = "request_query"
	WebhookBody  ctxKey = "webhook_body"
)

// StepSinkFunc records one Agent trace step.
type StepSinkFunc func(stepType, stepName, input, output, status string, latencyMS int64, errMsg string)

func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, TenantID, tenantID)
}

func TenantIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(TenantID).(string)
	return v
}

func WithRequestQuery(ctx context.Context, query string) context.Context {
	return context.WithValue(ctx, RequestQuery, query)
}

func RequestQueryFrom(ctx context.Context) string {
	v, _ := ctx.Value(RequestQuery).(string)
	return v
}

func WithWebhookBody(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, WebhookBody, body)
}

func WebhookBodyFrom(ctx context.Context) []byte {
	v, _ := ctx.Value(WebhookBody).([]byte)
	return v
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserID, userID)
}

func UserIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(UserID).(string)
	return v
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceID, traceID)
}

func TraceIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(TraceID).(string)
	return v
}

func WithRoles(ctx context.Context, roles []string) context.Context {
	return context.WithValue(ctx, Roles, roles)
}

func RolesFrom(ctx context.Context) []string {
	v, _ := ctx.Value(Roles).([]string)
	return v
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, SessionID, sessionID)
}

func SessionIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(SessionID).(string)
	return v
}

// WithToolSink attaches an evidence accumulator to the context. The Tool
// Gateway appends one entry per Invoke call so the Ops Agent can later expose
// a structured proof chain to the Portal.
func WithToolSink(ctx context.Context, sink *[]domain.Evidence) context.Context {
	return context.WithValue(ctx, ToolSink, sink)
}

func ToolSinkFrom(ctx context.Context) *[]domain.Evidence {
	v, _ := ctx.Value(ToolSink).(*[]domain.Evidence)
	return v
}

func WithStepSink(ctx context.Context, sink StepSinkFunc) context.Context {
	return context.WithValue(ctx, StepSink, sink)
}

func StepSinkFrom(ctx context.Context) StepSinkFunc {
	v, _ := ctx.Value(StepSink).(StepSinkFunc)
	return v
}
