package handler

import (
	"context"
	"time"

	v1 "wisesentinel-platform/api/v1"
	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"

	"github.com/gogf/gf/v2/frame/g"
)

// ControllerV1 implements Phase 1 API handlers.
type ControllerV1 struct {
	app *bootstrap.App
}

// NewV1 creates the v1 API controller.
func NewV1(app *bootstrap.App) *ControllerV1 {
	return &ControllerV1{app: app}
}

// AuthToken issues a JWT for development and integration testing.
func (c *ControllerV1) AuthToken(ctx context.Context, req *v1.AuthTokenReq) (*v1.AuthTokenRes, error) {
	devPassword := configx.String(ctx, "auth.dev_password", "DEV_PASSWORD")
	if devPassword == "" {
		devPassword = "dev123"
	}
	if req.Password != devPassword {
		return nil, apperr.ErrUnauthorized
	}

	userID := req.Username
	if userID == "" {
		userID = "dev_user"
	}

	token, expiresIn, err := auth.IssueToken(ctx, userID, []string{string(domain.RoleOperator)}, domain.DefaultTenantID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	return &v1.AuthTokenRes{
		AccessToken: token,
		ExpiresIn:   expiresIn,
		TokenType:   "Bearer",
	}, nil
}

// CreateSession creates a session placeholder (M2 will wire Memory service).
func (c *ControllerV1) CreateSession(ctx context.Context, req *v1.CreateSessionReq) (*v1.CreateSessionRes, error) {
	return nil, notImplemented("session")
}

// ListSessions lists sessions placeholder.
func (c *ControllerV1) ListSessions(ctx context.Context, req *v1.ListSessionsReq) (*v1.ListSessionsRes, error) {
	return nil, notImplemented("session")
}

// GetSessionMessages returns session messages placeholder.
func (c *ControllerV1) GetSessionMessages(ctx context.Context, req *v1.GetSessionMessagesReq) (*v1.GetSessionMessagesRes, error) {
	return nil, notImplemented("session")
}

// Chat synchronous chat placeholder.
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
	return nil, notImplemented("chat agent")
}

// ChatStream streaming chat placeholder.
func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (*v1.ChatStreamRes, error) {
	return nil, notImplemented("chat stream")
}

// OpsAnalyze ops analysis placeholder.
func (c *ControllerV1) OpsAnalyze(ctx context.Context, req *v1.OpsAnalyzeReq) (*v1.OpsAnalyzeRes, error) {
	return nil, notImplemented("ops agent")
}

// GetOpsTask returns ops task placeholder.
func (c *ControllerV1) GetOpsTask(ctx context.Context, req *v1.GetOpsTaskReq) (*v1.GetOpsTaskRes, error) {
	return nil, notImplemented("ops agent")
}

// AlertWebhook receives alert webhook placeholder.
func (c *ControllerV1) AlertWebhook(ctx context.Context, req *v1.AlertWebhookReq) (*v1.AlertWebhookRes, error) {
	return nil, notImplemented("ops webhook")
}

// ListApprovals lists approvals placeholder.
func (c *ControllerV1) ListApprovals(ctx context.Context, req *v1.ListApprovalsReq) (*v1.ListApprovalsRes, error) {
	return nil, notImplemented("approval")
}

// ApprovalDecision submits approval placeholder.
func (c *ControllerV1) ApprovalDecision(ctx context.Context, req *v1.ApprovalDecisionReq) (*v1.ApprovalDecisionRes, error) {
	return nil, notImplemented("approval")
}

// ListAgentConfigs lists agent configs placeholder.
func (c *ControllerV1) ListAgentConfigs(ctx context.Context, req *v1.ListAgentConfigsReq) (*v1.ListAgentConfigsRes, error) {
	return nil, notImplemented("admin")
}

// ActivateAgentConfig activates config placeholder.
func (c *ControllerV1) ActivateAgentConfig(ctx context.Context, req *v1.ActivateAgentConfigReq) (*v1.ActivateAgentConfigRes, error) {
	return nil, notImplemented("admin")
}

func notImplemented(feature string) error {
	return apperr.New(50101, 501, feature+" 将在后续里程碑实现")
}

// Ping returns platform metadata for smoke tests.
func (c *ControllerV1) Ping(ctx context.Context, _ *v1.PingReq) (v1.PingRes, error) {
	return v1.PingRes{
		"name":     g.Cfg().MustGet(ctx, "server.name", "wisesentinel-platform").String(),
		"version":  "m2",
		"trace_id": ctxkeys.TraceIDFrom(ctx),
		"time":     time.Now().UTC().Format(time.RFC3339),
	}, nil
}
