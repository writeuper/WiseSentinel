package ctxkeys

import "context"

type ctxKey string

const (
	TenantID  ctxKey = "tenant_id"
	UserID    ctxKey = "user_id"
	TraceID   ctxKey = "trace_id"
	Roles     ctxKey = "roles"
	SessionID ctxKey = "session_id"
)

func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, TenantID, tenantID)
}

func TenantIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(TenantID).(string)
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
