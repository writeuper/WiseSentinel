package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "wisesentinel-platform/api/v1"
	chatagent "wisesentinel-platform/internal/agent/chat"
	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/trace"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// ControllerV1 implements Phase 1 API handlers.
type ControllerV1 struct {
	app *bootstrap.App
}

// NewV1 creates the v1 API controller.
func NewV1(app *bootstrap.App) *ControllerV1 {
	return &ControllerV1{app: app}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// AuthToken issues a JWT for development and integration testing.
//
// In M5 we look up the user's assigned roles in ws_user_role so the JWT
// carries the actual RBAC roles. Falls back to "operator" if the user
// has no row in the role table (or the table is empty).
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

	roles := lookupUserRoles(ctx, userID)

	token, expiresIn, err := auth.IssueToken(ctx, userID, roles, domain.DefaultTenantID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	return &v1.AuthTokenRes{
		AccessToken: token,
		ExpiresIn:   expiresIn,
		TokenType:   "Bearer",
	}, nil
}

// lookupUserRoles reads all role strings for (tenant_id, user_id) from
// ws_user_role and returns them as a slice. Always includes at least
// "operator" so a freshly-seeded user can call /chat.
func lookupUserRoles(ctx context.Context, userID string) []string {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	if tenantID == "" {
		tenantID = domain.DefaultTenantID
	}
	type row struct {
		Role string
	}
	var rows []row
	err := g.DB().Model("ws_user_role").
		Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("user_id", userID).
		Fields("role").
		Scan(&rows)
	if err != nil || len(rows) == 0 {
		return []string{string(domain.RoleOperator)}
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Role)
	}
	return out
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

// CreateSession creates a new conversation session.
func (c *ControllerV1) CreateSession(ctx context.Context, req *v1.CreateSessionReq) (*v1.CreateSessionRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	if userID == "" {
		userID = "anonymous"
	}

	agentType := req.AgentType
	if agentType == "" {
		agentType = "chat"
	}

	sessionID, err := c.app.Memory.CreateSession(ctx, tenantID, userID,
		domain.WithSessionTitle(req.Title),
		domain.WithSessionAgentType(agentType),
	)
	if err != nil {
		return nil, err
	}

	return &v1.CreateSessionRes{
		SessionID: sessionID,
		Title:     req.Title,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// ListSessions lists user sessions.
func (c *ControllerV1) ListSessions(ctx context.Context, req *v1.ListSessionsReq) (*v1.ListSessionsRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	if userID == "" {
		userID = "anonymous"
	}

	items, total, err := c.app.Memory.ListSessions(ctx, tenantID, userID, req.Page, req.Size)
	if err != nil {
		return nil, err
	}

	sessionItems := make([]v1.SessionItem, len(items))
	for i, item := range items {
		sessionItems[i] = v1.SessionItem{
			SessionID: item.SessionID,
			Title:     item.Title,
			AgentType: item.AgentType,
			UpdatedAt: item.UpdatedAt,
		}
	}

	return &v1.ListSessionsRes{Items: sessionItems, Total: total}, nil
}

// GetSessionMessages returns messages for a session.
func (c *ControllerV1) GetSessionMessages(ctx context.Context, req *v1.GetSessionMessagesReq) (*v1.GetSessionMessagesRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)

	msgs, err := c.app.Memory.GetHistory(ctx, tenantID, req.SessionID)
	if err != nil {
		return nil, err
	}

	items := make([]v1.MessageItem, 0, len(msgs))
	for _, msg := range msgs {
		ts := msg.Timestamp
		if ts == "" {
			ts = time.Now().UTC().Format(time.RFC3339)
		}
		items = append(items, v1.MessageItem{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: ts,
		})
	}

	return &v1.GetSessionMessagesRes{
		SessionID: req.SessionID,
		Messages:  items,
	}, nil
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------

// Chat handles synchronous chat requests.
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	// Validate session
	session, err := c.app.Memory.GetSession(ctx, tenantID, req.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperr.ErrNotFound
	}

	// Get history
	history, err := c.app.Memory.GetHistory(ctx, tenantID, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Build options
	options := domain.ChatOptions{}
	if req.Options != nil {
		options.EnableRAG = req.Options.EnableRAG
		options.EnableTools = req.Options.EnableTools
	}

	// Invoke chat agent
	result, err := c.app.ChatAgent.Invoke(ctx, &chatagent.UserMessage{
		TenantID:  tenantID,
		SessionID: req.SessionID,
		UserID:    userID,
		Query:     req.Question,
		History:   history,
		Options:   options,
	})
	if err != nil {
		return nil, err
	}

	// Persist messages
	userMsg := &domain.Message{Role: "user", Content: req.Question}
	assistantMsg := &domain.Message{Role: "assistant", Content: result.Answer}
	if err := c.app.Memory.AppendMessages(ctx, tenantID, req.SessionID, userMsg, assistantMsg); err != nil {
		return nil, err
	}

	// Update session title from first user message
	if len(history) == 0 {
		title := req.Question
		if len([]rune(title)) > 30 {
			title = string([]rune(title)[:30]) + "..."
		}
		_ = c.app.Memory.UpdateSessionTitle(ctx, tenantID, req.SessionID, title)
	}

	return &v1.ChatRes{
		SessionID: req.SessionID,
		Answer:    result.Answer,
		TraceID:   traceID,
	}, nil
}

// ChatStream handles SSE streaming chat.
func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (*v1.ChatStreamRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)

	// Validate session
	session, err := c.app.Memory.GetSession(ctx, tenantID, req.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperr.ErrNotFound
	}

	// Get history
	history, err := c.app.Memory.GetHistory(ctx, tenantID, req.SessionID)
	if err != nil {
		return nil, err
	}

	options := domain.ChatOptions{}
	if req.Options != nil {
		options.EnableRAG = req.Options.EnableRAG
		options.EnableTools = req.Options.EnableTools
	}

	// Get the underlying HTTP response writer for SSE
	r := ghttp.RequestFromCtx(ctx)
	if r == nil {
		return nil, apperr.ErrInternal
	}

	// Set SSE headers
	r.Response.Header().Set("Content-Type", "text/event-stream")
	r.Response.Header().Set("Cache-Control", "no-cache")
	r.Response.Header().Set("Connection", "keep-alive")
	r.Response.WriteHeader(200)

	// Start streaming
	events, err := c.app.ChatAgent.Stream(ctx, &chatagent.UserMessage{
		TenantID:  tenantID,
		SessionID: req.SessionID,
		UserID:    userID,
		Query:     req.Question,
		History:   history,
		Options:   options,
	})
	if err != nil {
		return nil, err
	}

	var fullAnswer strings.Builder
	for event := range events {
		writeSSEToResponse(r.Response, event.Type, event.Data)
		if event.Type == "message" {
			fullAnswer.WriteString(event.Data)
		}
	}

	// Flush after streaming completes
	r.Response.Flush()

	// Persist messages after streaming completes
	userMsg := &domain.Message{Role: "user", Content: req.Question}
	assistantMsg := &domain.Message{Role: "assistant", Content: fullAnswer.String()}
	_ = c.app.Memory.AppendMessages(ctx, tenantID, req.SessionID, userMsg, assistantMsg)

	// Update session title
	if len(history) == 0 {
		title := req.Question
		if len([]rune(title)) > 30 {
			title = string([]rune(title)[:30]) + "..."
		}
		_ = c.app.Memory.UpdateSessionTitle(ctx, tenantID, req.SessionID, title)
	}

	return nil, nil
}

func writeSSEToResponse(res *ghttp.Response, event, data string) {
	_, _ = res.WriteString(fmt.Sprintf("event: %s\n", event))
	_, _ = res.WriteString(fmt.Sprintf("data: %s\n\n", data))
	res.Flush()
}

// ---------------------------------------------------------------------------
// Ops
// ---------------------------------------------------------------------------

// OpsAnalyze triggers alert analysis.
func (c *ControllerV1) OpsAnalyze(ctx context.Context, req *v1.OpsAnalyzeReq) (*v1.OpsAnalyzeRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)

	async := false
	maxIterations := 20
	if req.Options != nil {
		async = req.Options.Async
		if req.Options.MaxIterations > 0 {
			maxIterations = req.Options.MaxIterations
		}
	}

	result, err := c.app.OpsAgent.OpsAnalyze(ctx, &domain.OpsAgentRequest{
		TenantID:      tenantID,
		UserID:        userID,
		Query:         req.Query,
		MaxIterations: maxIterations,
		Async:         async,
	})
	if err != nil {
		return nil, err
	}

	return &v1.OpsAnalyzeRes{
		TaskID:  result.TaskID,
		Status:  string(result.Status),
		Result:  result.Result,
		Detail:  result.Detail,
		TraceID: result.TraceID,
	}, nil
}

// GetOpsTask returns an ops task status.
func (c *ControllerV1) GetOpsTask(ctx context.Context, req *v1.GetOpsTaskReq) (*v1.GetOpsTaskRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)

	// Check if OpsAgent is the placeholder (has GetTaskResult method)
	type taskGetter interface {
		GetTaskResult(ctx context.Context, tenantID, taskID string) (*domain.OpsAgentResponse, error)
	}
	if getter, ok := c.app.OpsAgent.(taskGetter); ok {
		result, err := getter.GetTaskResult(ctx, tenantID, req.TaskID)
		if err != nil {
			return nil, err
		}
		return &v1.GetOpsTaskRes{
			TaskID: result.TaskID,
			Status: string(result.Status),
			Result: result.Result,
			Detail: result.Detail,
		}, nil
	}

	return nil, apperr.New(50101, 501, "Ops 任务查询将在 M4 实现")
}

// ListOpsTasks returns a page of recent ops tasks for the tenant (M5).
type opsTaskLister interface {
	ListOpsTasks(ctx context.Context, tenantID, statusFilter string, page, size int) (items []opsTaskSummary, total int, err error)
}

func (c *ControllerV1) ListOpsTasks(ctx context.Context, req *v1.ListOpsTasksReq) (*v1.ListOpsTasksRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	// Duck-type to the concrete ops agent. The interface keeps the
	// controller independent of the agent package while still returning
	// a typed summary list.
	l, ok := c.app.OpsAgent.(opsTaskLister)
	if !ok {
		return &v1.ListOpsTasksRes{Items: []v1.OpsTaskSummary{}, Total: 0}, nil
	}
	items, total, err := l.ListOpsTasks(ctx, tenantID, req.Status, req.Page, req.Size)
	if err != nil {
		return nil, err
	}
	out := make([]v1.OpsTaskSummary, 0, len(items))
	for _, it := range items {
		out = append(out, v1.OpsTaskSummary{
			TaskID:      it.TaskID,
			Status:      it.Status,
			TriggerType: it.TriggerType,
			CreatedAt:   it.CreatedAt,
			CreatedBy:   it.CreatedBy,
		})
	}
	return &v1.ListOpsTasksRes{Items: out, Total: total}, nil
}

// CurrentUser returns the identity decoded from the current JWT.
func (c *ControllerV1) CurrentUser(ctx context.Context, _ *v1.CurrentUserReq) (*v1.CurrentUserRes, error) {
	return &v1.CurrentUserRes{
		Username: ctxkeys.UserIDFrom(ctx),
		TenantID: ctxkeys.TenantIDFrom(ctx),
		Roles:    ctxkeys.RolesFrom(ctx),
	}, nil
}

// opsTaskSummary is the internal type asserted from OpsAgent.ListOpsTasks.
type opsTaskSummary struct {
	TaskID      string
	Status      string
	TriggerType string
	CreatedAt   string
	CreatedBy   string
}

// AlertWebhook receives Alertmanager webhook events.
func (c *ControllerV1) AlertWebhook(ctx context.Context, req *v1.AlertWebhookReq) (*v1.AlertWebhookRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)

	// Build a query from the alert
	var alertDescs []string
	for _, alert := range req.Alerts {
		name := alert.Labels["alertname"]
		desc := alert.Annotations["description"]
		if desc == "" {
			desc = name
		}
		alertDescs = append(alertDescs, desc)
	}
	query := fmt.Sprintf("检测到告警：%s\n请按 Runbook 分析并生成报告。", strings.Join(alertDescs, "; "))

	result, err := c.app.OpsAgent.OpsAnalyze(ctx, &domain.OpsAgentRequest{
		TenantID:      tenantID,
		UserID:        userID,
		Query:         query,
		MaxIterations: 20,
		Async:         true,
	})
	if err != nil {
		return nil, err
	}

	return &v1.AlertWebhookRes{
		TaskID: result.TaskID,
		Status: string(result.Status),
	}, nil
}

// ---------------------------------------------------------------------------
// Knowledge
// ---------------------------------------------------------------------------

// UploadDocument handles document upload and indexing (implemented in knowledge.go).
// NOTE: This method is already implemented in knowledge.go and will be resolved
// by GoFrame's method binding. The empty body here is intentional - GoFrame
// will use the knowledge.go implementation.

// ListDocuments lists knowledge documents (implemented in knowledge.go).

// DeleteDocument soft-deletes a document (implemented in knowledge.go).

// GetIndexTask returns index task status (implemented in knowledge.go).

// ---------------------------------------------------------------------------
// Approval
// ---------------------------------------------------------------------------

// ListApprovals lists pending approvals.
func (c *ControllerV1) ListApprovals(ctx context.Context, req *v1.ListApprovalsReq) (*v1.ListApprovalsRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)

	var items []v1.ApprovalItem
	total := 0

	// Query approvals from DB
	type approvalRow struct {
		ApprovalID   string `json:"approval_id"`
		TaskID       string `json:"task_id"`
		ApprovalType string `json:"approval_type"`
		Status       string `json:"status"`
		ExpiredAt    string `json:"expired_at"`
	}
	var rows []approvalRow
	err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("status", "pending").
		OrderAsc("created_at").
		Page(req.Page, req.Size).
		Scan(&rows)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	for _, row := range rows {
		items = append(items, v1.ApprovalItem{
			ApprovalID:   row.ApprovalID,
			TaskID:       row.TaskID,
			ApprovalType: row.ApprovalType,
			Status:       row.Status,
			ExpiredAt:    row.ExpiredAt,
		})
	}

	// Get total count
	count, err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("status", "pending").
		Count()
	if err == nil {
		total = count
	}

	return &v1.ListApprovalsRes{Items: items, Total: total}, nil
}

// ApprovalDecision submits an approval decision.
func (c *ControllerV1) ApprovalDecision(ctx context.Context, req *v1.ApprovalDecisionReq) (*v1.ApprovalDecisionRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)

	now := time.Now()
	_, err := g.DB().Model("ws_approval").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("approval_id", req.ApprovalID).
		Where("status", "pending").
		Data(g.Map{
			"status":     req.Decision,
			"approver_id": userID,
			"comment":    req.Comment,
			"updated_at": now,
		}).
		Update()
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	return &v1.ApprovalDecisionRes{
		ApprovalID: req.ApprovalID,
		Status:     req.Decision,
	}, nil
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

// ListAgentConfigs lists agent configuration versions.
func (c *ControllerV1) ListAgentConfigs(ctx context.Context, req *v1.ListAgentConfigsReq) (*v1.ListAgentConfigsRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)

	type configRow struct {
		AgentType string `json:"agent_type"`
		Version   string `json:"version"`
		IsActive  int    `json:"is_active"`
		CreatedAt string `json:"created_at"`
	}
	var rows []configRow
	err := g.DB().Model("ws_agent_config").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("agent_type", req.AgentType).
		OrderDesc("created_at").
		Scan(&rows)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	items := make([]v1.AgentConfigItem, len(rows))
	for i, row := range rows {
		items[i] = v1.AgentConfigItem{
			AgentType: row.AgentType,
			Version:   row.Version,
			IsActive:  row.IsActive == 1,
			CreatedAt: row.CreatedAt,
		}
	}

	return &v1.ListAgentConfigsRes{Items: items}, nil
}

// ActivateAgentConfig activates a config version.
func (c *ControllerV1) ActivateAgentConfig(ctx context.Context, req *v1.ActivateAgentConfigReq) (*v1.ActivateAgentConfigRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)

	// Deactivate all active configs for this agent type
	_, err := g.DB().Model("ws_agent_config").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("agent_type", req.AgentType).
		Where("is_active", 1).
		Data(g.Map{"is_active": 0}).
		Update()
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	// Activate the requested version
	result, err := g.DB().Model("ws_agent_config").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("agent_type", req.AgentType).
		Where("version", req.Version).
		Data(g.Map{"is_active": 1}).
		Update()
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	rowsAffected, _ := result.RowsAffected()
	isActive := rowsAffected > 0

	return &v1.ActivateAgentConfigRes{
		AgentType: req.AgentType,
		Version:   req.Version,
		IsActive:  isActive,
	}, nil
}

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

// Ping returns platform metadata for smoke tests.
func (c *ControllerV1) Ping(ctx context.Context, _ *v1.PingReq) (v1.PingRes, error) {
	return v1.PingRes{
		"name":     g.Cfg().MustGet(ctx, "server.name", "wisesentinel-platform").String(),
		"version":  "m3",
		"trace_id": ctxkeys.TraceIDFrom(ctx),
		"time":     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func notImplemented(feature string) error {
	return apperr.New(50101, 501, feature+" 将在后续里程碑实现")
}

