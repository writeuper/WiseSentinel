package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	v1 "wisesentinel-platform/api/v1"
	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/gateway/auth"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/configx"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/pkg/trace"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
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
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return nil, apperr.ErrUnauthorized
	}
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

	// The development endpoint always issues a default-tenant token. Resolve
	// roles in that same tenant rather than trusting X-Tenant-ID on a public
	// login request.
	roles := lookupUserRoles(ctx, domain.DefaultTenantID, userID)

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
func lookupUserRoles(ctx context.Context, tenantID, userID string) []string {
	type row struct {
		Role string
	}
	// The development login accepts either the stable user_id or the seeded
	// email address. Resolve email to user_id before reading roles; otherwise
	// role-specific demo accounts would silently fall back to operator.
	userIDs := []string{userID}
	var users []struct {
		UserID string `json:"user_id"`
	}
	db := g.DB()
	if err := db.Model("ws_user").Ctx(ctx).
		Where("tenant_id", tenantID).Where("email", userID).
		Fields("user_id").Scan(&users); err == nil {
		for _, user := range users {
			if user.UserID != "" && user.UserID != userID {
				userIDs = append(userIDs, user.UserID)
			}
		}
	}
	var rows []row
	err := db.Model("ws_user_role").
		Ctx(ctx).
		Where("tenant_id", tenantID).
		WhereIn("user_id", userIDs).
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
	if err := validateBoundedText(req.Title, maxSessionTitleRunes); err != nil {
		return nil, err
	}
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
	if err := c.requireSessionRead(ctx, tenantID, req.SessionID); err != nil {
		return nil, err
	}

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

// DeleteSession deactivates one session after the same object-level access
// check used by chat and history reads. The server, not the client, remains
// the authorization authority.
func (c *ControllerV1) DeleteSession(ctx context.Context, req *v1.DeleteSessionReq) (*v1.DeleteSessionRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	if err := c.requireSessionRead(ctx, tenantID, req.SessionID); err != nil {
		return nil, err
	}
	if err := c.app.Memory.DeleteSession(ctx, tenantID, req.SessionID); err != nil {
		return nil, err
	}
	return &v1.DeleteSessionRes{SessionID: req.SessionID, Status: "deleted"}, nil
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------

// Chat handles synchronous chat requests.
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
	if err := validateBoundedText(req.Question, maxChatQuestionRunes); err != nil {
		return nil, err
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	if err := c.requireSessionRead(ctx, tenantID, req.SessionID); err != nil {
		return nil, err
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
	turn, replay, err := c.claimSynchronousChatTurn(ctx, tenantID, userID, req.SessionID, req.Question, options)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return replay, nil
	}
	failTurn := func(errorClass string) {
		if turn != nil {
			_ = c.app.ChatTurnRepo.Fail(ctx, turn, errorClass)
		}
	}

	// Invoke chat agent
	result, err := c.app.ChatAgent.Invoke(ctx, &domain.ChatAgentRequest{
		TenantID:  tenantID,
		SessionID: req.SessionID,
		UserID:    userID,
		Query:     req.Question,
		History:   history,
		Options:   options,
	})
	if err != nil {
		failTurn("agent_error")
		return nil, err
	}

	// Session history and the synchronous API are observability/presentation
	// boundaries. Keep the request raw only for the in-flight model call.
	safeQuestion := redact.Summary(req.Question, 4000)
	safeAnswer := redact.Summary(result.Answer, 8000)
	userMsg := &domain.Message{Role: "user", Content: safeQuestion}
	assistantMsg := &domain.Message{Role: "assistant", Content: safeAnswer}
	if err := c.app.Memory.AppendMessages(ctx, tenantID, req.SessionID, userMsg, assistantMsg); err != nil {
		failTurn("session_persist_error")
		return nil, err
	}

	// Update session title from first user message
	if len(history) == 0 {
		title := safeQuestion
		if len([]rune(title)) > 30 {
			title = string([]rune(title)[:30]) + "..."
		}
		_ = c.app.Memory.UpdateSessionTitle(ctx, tenantID, req.SessionID, title)
	}

	citations := make([]v1.CitationItem, 0, len(result.Citations))
	for _, citation := range result.Citations {
		citations = append(citations, v1.CitationItem{
			DocID:   citation.DocID,
			ChunkID: citation.ChunkID,
			Source:  redact.Summary(citation.Source, 500),
			Snippet: redact.Summary(citation.Snippet, 500),
			Version: redact.Summary(citation.Version, 100),
		})
	}
	toolCalls := make([]v1.ToolCallSummary, 0, len(result.ToolCalls))
	for _, call := range result.ToolCalls {
		toolCalls = append(toolCalls, v1.ToolCallSummary{
			Tool:      call.Tool,
			Status:    call.Status,
			LatencyMS: call.LatencyMS,
		})
	}

	response := &v1.ChatRes{
		SessionID: req.SessionID,
		Answer:    safeAnswer,
		Citations: citations,
		ToolCalls: toolCalls,
		TraceID:   traceID,
	}
	if turn != nil {
		payload, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			failTurn("response_projection_error")
			return nil, apperr.ErrInternal
		}
		saved, saveErr := c.app.ChatTurnRepo.Succeed(ctx, turn, string(payload), traceID)
		if saveErr != nil || !saved {
			return nil, apperr.ErrInternal
		}
	}
	return response, nil
}

const (
	minChatIdempotencyKeyLength = 36
	maxChatIdempotencyKeyLength = 128
)

// claimSynchronousChatTurn creates the durable execution owner for a supplied
// Idempotency-Key or returns its completed safe response. SSE intentionally
// does not call this helper until event-sequence replay is implemented.
func (c *ControllerV1) claimSynchronousChatTurn(ctx context.Context, tenantID, userID, sessionID, question string, options domain.ChatOptions) (*repository.ChatTurn, *v1.ChatRes, error) {
	r := ghttp.RequestFromCtx(ctx)
	if r == nil {
		return nil, nil, nil
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return nil, nil, nil
	}
	if !validChatIdempotencyKey(key) || c.app.ChatTurnRepo == nil {
		return nil, nil, apperr.ErrBadRequest
	}
	payload, err := json.Marshal(struct {
		Question    string `json:"question"`
		EnableRAG   bool   `json:"enable_rag"`
		EnableTools bool   `json:"enable_tools"`
	}{Question: question, EnableRAG: options.EnableRAG, EnableTools: options.EnableTools})
	if err != nil {
		return nil, nil, apperr.ErrInternal
	}
	hash := sha256.Sum256(payload)
	turn := &repository.ChatTurn{
		TenantID:       tenantID,
		UserID:         userID,
		SessionID:      sessionID,
		IdempotencyKey: key,
		RequestHash:    hex.EncodeToString(hash[:]),
		ExecutionToken: uuid.NewString(),
	}
	stored, created, err := c.app.ChatTurnRepo.Claim(ctx, turn)
	if err != nil {
		return nil, nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if created {
		return turn, nil, nil
	}
	if stored == nil || stored.RequestHash != turn.RequestHash {
		return nil, nil, apperr.ErrConflict
	}
	if stored.Status != repository.ChatTurnSucceeded || stored.ResponseJSON == "" {
		return nil, nil, apperr.ErrConflict
	}
	var replay v1.ChatRes
	if err := json.Unmarshal([]byte(stored.ResponseJSON), &replay); err != nil || replay.SessionID != sessionID {
		return nil, nil, apperr.ErrInternal
	}
	return nil, &replay, nil
}

func validChatIdempotencyKey(key string) bool {
	if len(key) < minChatIdempotencyKeyLength || len(key) > maxChatIdempotencyKeyLength {
		return false
	}
	for _, char := range key {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// ChatStream handles SSE streaming chat.
func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (*v1.ChatStreamRes, error) {
	if err := validateBoundedText(req.Question, maxChatQuestionRunes); err != nil {
		return nil, err
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)

	if err := c.requireSessionRead(ctx, tenantID, req.SessionID); err != nil {
		return nil, err
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
	reader, err := c.app.ChatAgent.Stream(ctx, &domain.ChatAgentRequest{
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
	defer reader.Close()

	stream := consumeChatStream(reader, func(event, data string) {
		writeSSEToResponse(r.Response, event, data)
	})

	// Flush after streaming completes
	r.Response.Flush()

	// Do not retain a partial answer after a provider failure or client-side
	// cancellation. A future question must never inherit a silently truncated
	// model response as conversation history.
	if stream.failed || !stream.finished || ctx.Err() != nil {
		return nil, nil
	}

	// Persist messages after a clean streaming completion.
	userMsg := &domain.Message{Role: "user", Content: redact.Summary(req.Question, 4000)}
	assistantMsg := &domain.Message{Role: "assistant", Content: stream.answer}
	if err := c.app.Memory.AppendMessages(ctx, tenantID, req.SessionID, userMsg, assistantMsg); err != nil {
		// The model output has already been delivered, but it must not be
		// represented as a completed turn when the durable conversation window
		// could not be updated. Do not expose storage diagnostics to the browser.
		writeSSEToResponse(r.Response, "error", "会话状态保存失败，请稍后重试。")
		return nil, nil
	}

	// Update session title
	if len(history) == 0 {
		title := redact.Summary(req.Question, 1000)
		if len([]rune(title)) > 30 {
			title = string([]rune(title)[:30]) + "..."
		}
		_ = c.app.Memory.UpdateSessionTitle(ctx, tenantID, req.SessionID, title)
	}

	// The upstream reader's done marker is intentionally emitted only after
	// the complete turn has been saved. A done event is the browser's contract
	// that a later history read can include this response.
	writeSSEToResponse(r.Response, "done", stream.doneData)

	return nil, nil
}

type chatStreamCompletion struct {
	answer   string
	doneData string
	failed   bool
	finished bool
}

// consumeChatStream projects stream payloads at the HTTP boundary and delays
// the terminal done marker until the caller has persisted the complete turn.
// This keeps model delivery and session-history completion from diverging.
func consumeChatStream(reader domain.StreamReader, emit func(event, data string)) chatStreamCompletion {
	var answer strings.Builder
	completion := chatStreamCompletion{}
	for {
		event, data, ok := reader.Next()
		if !ok {
			break
		}
		safeData := redact.Summary(data, 8000)
		if event == "tool" {
			safeData = redact.Summary(redact.JSON(data), 8000)
		} else if event == "citation" {
			safeData = safeSSECitation(data)
		}
		if event == "done" {
			completion.doneData = safeData
			completion.finished = true
			continue
		}
		emit(event, safeData)
		if event == "error" {
			completion.failed = true
		}
		if event == "message" {
			answer.WriteString(safeData)
		}
	}
	completion.answer = answer.String()
	return completion
}

func safeSSECitation(data string) string {
	var citation domain.Citation
	if err := json.Unmarshal([]byte(data), &citation); err != nil {
		return `{}`
	}
	payload, err := json.Marshal(v1.CitationItem{
		DocID:   citation.DocID,
		ChunkID: citation.ChunkID,
		Source:  redact.Summary(citation.Source, 500),
		Snippet: redact.Summary(citation.Snippet, 500),
		Version: redact.Summary(citation.Version, 100),
	})
	if err != nil {
		return `{}`
	}
	return string(payload)
}

func writeSSEToResponse(res *ghttp.Response, event, data string) {
	_, _ = res.WriteString(formatSSE(event, data))
	res.Flush()
}

// formatSSE keeps every payload line inside the SSE data field. Writing a
// multi-line model chunk as one raw `data:` line would let later lines be
// interpreted as SSE framing rather than user content.
func formatSSE(event, data string) string {
	var builder strings.Builder
	builder.WriteString("event: ")
	builder.WriteString(event)
	builder.WriteByte('\n')
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		builder.WriteString("data: ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	builder.WriteByte('\n')
	return builder.String()
}

// ---------------------------------------------------------------------------
// Ops
// ---------------------------------------------------------------------------

// OpsAnalyze triggers alert analysis.
func (c *ControllerV1) OpsAnalyze(ctx context.Context, req *v1.OpsAnalyzeReq) (*v1.OpsAnalyzeRes, error) {
	if err := validateBoundedText(req.Query, maxOpsQueryRunes); err != nil {
		return nil, err
	}
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

	result, err := c.app.OpsAgent.Analyze(ctx, &domain.OpsAgentRequest{
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
		TaskID:     result.TaskID,
		Status:     string(result.Status),
		Result:     safeOpsResult(result.Status, req.Query),
		Detail:     toSafeDetails(result.Detail),
		TraceID:    result.TraceID,
		Evidence:   toOpsEvidence(result.Evidence),
		Conclusion: toOpsConclusion(result.Conclusion),
		Timing:     toOpsTiming(result.Timing),
	}, nil
}

// GetOpsTask returns an ops task status.
func (c *ControllerV1) GetOpsTask(ctx context.Context, req *v1.GetOpsTaskReq) (*v1.GetOpsTaskRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	task, err := c.app.OpsTaskRepo.Get(ctx, tenantID, req.TaskID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if task == nil || !canReadOwnedResource(ctx, task.CreatedBy) {
		return nil, apperr.ErrNotFound
	}

	result, err := c.app.OpsAgent.GetTaskResult(ctx, tenantID, req.TaskID)
	if err != nil {
		return nil, err
	}

	return &v1.GetOpsTaskRes{
		TaskID:     result.TaskID,
		Status:     string(result.Status),
		Result:     safeOpsResult(result.Status, task.InputQuery),
		Detail:     toSafeDetails(result.Detail),
		Evidence:   toOpsEvidence(result.Evidence),
		Conclusion: toOpsConclusion(result.Conclusion),
		Timing:     toOpsTiming(result.Timing),
	}, nil
}

// ListOpsTasks returns a page of recent ops tasks for the tenant.
func (c *ControllerV1) ListOpsTasks(ctx context.Context, req *v1.ListOpsTasksReq) (*v1.ListOpsTasksRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	var (
		items []*repository.OpsTask
		total int
		err   error
	)
	if canReadAllTenantResources(ctx) {
		items, total, err = c.app.OpsTaskRepo.ListByTenant(ctx, tenantID, req.Status, req.Page, req.Size)
	} else {
		items, total, err = c.app.OpsTaskRepo.ListByTenantAndCreator(ctx, tenantID, ctxkeys.UserIDFrom(ctx), req.Status, req.Page, req.Size)
	}
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	out := make([]v1.OpsTaskSummary, 0, len(items))
	for _, it := range items {
		out = append(out, v1.OpsTaskSummary{
			TaskID:      it.TaskID,
			Status:      it.Status,
			TriggerType: it.TriggerType,
			CreatedAt:   it.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
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

// AlertWebhook receives Alertmanager webhook events.
func (c *ControllerV1) AlertWebhook(ctx context.Context, req *v1.AlertWebhookReq) (*v1.AlertWebhookRes, error) {
	return c.handleAlertmanager(ctx, req.Receiver, req.Status, req.GroupKey, req.CommonLabels, req.CommonAnnotations, req.ExternalURL, req.Version, req.Alerts)
}

// AlertmanagerWebhook handles the signed internal Alertmanager endpoint.
func (c *ControllerV1) AlertmanagerWebhook(ctx context.Context, req *v1.AlertmanagerWebhookReq) (*v1.AlertWebhookRes, error) {
	return c.handleAlertmanager(ctx, req.Receiver, req.Status, req.GroupKey, req.CommonLabels, req.CommonAnnotations, req.ExternalURL, req.Version, req.Alerts)
}

func (c *ControllerV1) handleAlertmanager(ctx context.Context, receiver, status, groupKey string, commonLabels, commonAnnotations map[string]string, externalURL, version string, alerts []v1.AlertEvent) (*v1.AlertWebhookRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	if tenantID == "" {
		tenantID = domain.DefaultTenantID
	}
	body := ctxkeys.WebhookBodyFrom(ctx)
	if len(body) == 0 {
		body = []byte(repository.MarshalAlertPayload(g.Map{"status": status, "alerts": alerts}))
	}
	eventID := strings.TrimSpace(hex.EncodeToString(sha256.New().Sum(body)))
	hash := sha256.Sum256(body)
	eventID = hex.EncodeToString(hash[:])
	incidentKey := groupKey
	if incidentKey == "" && len(alerts) > 0 {
		incidentKey = alerts[0].Fingerprint
	}
	if c.app.AlertEventRepo != nil {
		if existing, err := c.app.AlertEventRepo.Get(ctx, tenantID, eventID); err == nil && existing != nil {
			return &v1.AlertWebhookRes{TaskID: existing.TaskID, Status: existing.Status}, nil
		}
	}
	var alertDescs []string
	for _, alert := range alerts {
		name := alert.Labels["alertname"]
		desc := alert.Annotations["description"]
		if desc == "" {
			desc = name
		}
		// Alert annotations are external input and are frequently copied from
		// logs. Pass only a safe diagnostic projection into the Agent prompt.
		alertDescs = append(alertDescs, redact.Summary(desc, 1000))
	}
	query := fmt.Sprintf("检测到告警：%s\n请按 Runbook 分析并生成报告。", strings.Join(alertDescs, "; "))
	if status == "resolved" {
		var taskID string
		if c.app.AlertEventRepo != nil {
			if existing, _ := c.app.AlertEventRepo.GetByIncident(ctx, tenantID, incidentKey); existing != nil {
				taskID = existing.TaskID
			}
			_ = c.app.AlertEventRepo.MarkResolved(ctx, tenantID, incidentKey, time.Now())
		}
		return &v1.AlertWebhookRes{TaskID: taskID, Status: "resolved"}, nil
	}
	// Reserve the event before invoking the Agent. The unique tenant/event key
	// makes concurrent Alertmanager redeliveries converge on one task instead
	// of starting multiple model/tool workflows. Bind the reserved task ID into
	// the Agent request so the reservation and durable task share one identity.
	reservedTaskID := "ops_" + uuid.NewString()
	event := &repository.AlertEvent{TenantID: tenantID, EventID: eventID, IncidentKey: incidentKey, Receiver: receiver, GroupKey: groupKey, Status: status, PayloadJSON: alertEventProjection(receiver, status, groupKey, commonLabels, commonAnnotations, externalURL, version, alerts), TaskID: reservedTaskID, ReceivedAt: time.Now()}
	if c.app.AlertEventRepo != nil {
		if err := c.app.AlertEventRepo.Create(ctx, event); err != nil {
			if existing, getErr := c.app.AlertEventRepo.Get(ctx, tenantID, eventID); getErr == nil && existing != nil {
				return &v1.AlertWebhookRes{TaskID: existing.TaskID, Status: existing.Status}, nil
			}
			return nil, err
		}
	}
	result, err := c.app.OpsAgent.Analyze(ctx, &domain.OpsAgentRequest{TenantID: tenantID, TaskID: reservedTaskID, Query: query, MaxIterations: 20, Async: true, TriggerType: "webhook"})
	if err != nil {
		// Do not leave a permanently deduplicated event pointing at a task that
		// was never created; a later delivery must be able to retry safely.
		if c.app.AlertEventRepo != nil {
			_ = c.app.AlertEventRepo.Delete(ctx, tenantID, eventID)
		}
		return nil, err
	}
	if result.TaskID == "" || result.Status == domain.OpsTaskFailed {
		if c.app.AlertEventRepo != nil {
			_ = c.app.AlertEventRepo.Delete(ctx, tenantID, eventID)
		}
		return nil, apperr.ErrAgentFailed
	}
	// Keep the event/task binding explicit even if a future Agent implementation
	// normalizes or returns a different response shape.
	if c.app.AlertEventRepo != nil && result.TaskID != reservedTaskID {
		_ = c.app.AlertEventRepo.UpdateTaskID(ctx, tenantID, eventID, result.TaskID)
	}
	return &v1.AlertWebhookRes{TaskID: result.TaskID, Status: string(result.Status)}, nil
}

var alertLabelAllowlist = map[string]struct{}{
	"alertname": {}, "cluster": {}, "environment": {}, "instance": {},
	"job": {}, "namespace": {}, "service": {}, "severity": {},
}

var alertAnnotationAllowlist = map[string]struct{}{
	"description": {}, "runbook_url": {}, "summary": {},
}

func alertEventProjection(receiver, status, groupKey string, commonLabels, commonAnnotations map[string]string, externalURL, version string, alerts []v1.AlertEvent) string {
	projectedAlerts := make([]map[string]any, 0, len(alerts))
	for _, alert := range alerts {
		projectedAlerts = append(projectedAlerts, map[string]any{
			"status":      redact.Summary(alert.Status, 64),
			"labels":      filterAlertFields(alert.Labels, alertLabelAllowlist),
			"annotations": filterAlertFields(alert.Annotations, alertAnnotationAllowlist),
			"starts_at":   redact.Summary(alert.StartsAt, 64),
			"ends_at":     redact.Summary(alert.EndsAt, 64),
			"fingerprint": redact.Summary(alert.Fingerprint, 256),
		})
	}
	payload := map[string]any{
		"receiver":           redact.Summary(receiver, 256),
		"status":             redact.Summary(status, 64),
		"group_key":          redact.Summary(groupKey, 512),
		"common_labels":      filterAlertFields(commonLabels, alertLabelAllowlist),
		"common_annotations": filterAlertFields(commonAnnotations, alertAnnotationAllowlist),
		"external_url":       redact.Summary(externalURL, 1000),
		"version":            redact.Summary(version, 64),
		"alerts":             projectedAlerts,
	}
	return redact.JSON(repository.MarshalAlertPayload(payload))
}

func filterAlertFields(values map[string]string, allowlist map[string]struct{}) map[string]string {
	out := make(map[string]string)
	for key, value := range values {
		if _, ok := allowlist[strings.ToLower(key)]; ok {
			out[key] = redact.Summary(value, 1000)
		}
	}
	return out
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
	if err := c.app.ApprovalRepo.ExpireStale(ctx); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	rows, total, err := c.app.ApprovalRepo.ListPending(ctx, tenantID, req.Page, req.Size)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	items := make([]v1.ApprovalItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, v1.ApprovalItem{
			ApprovalID:   row.ApprovalID,
			TaskID:       row.TaskID,
			ApprovalType: row.ApprovalType,
			Status:       row.Status,
			ExpiredAt:    row.ExpiredAt.Format(time.RFC3339),
			Target:       approvalTargetProjection(row),
		})
	}

	return &v1.ListApprovalsRes{Items: items, Total: total}, nil
}

// approvalTargetProjection exposes only the data a second administrator needs
// to assess a vector-GC redrive. The stored approval payload remains private
// because it can contain requester identity and an operational reason.
func approvalTargetProjection(approval *repository.Approval) *v1.ApprovalTarget {
	if approval == nil || approval.ApprovalType != repository.VectorGCRedriveApprovalType {
		return nil
	}
	var payload struct {
		DocID     string `json:"doc_id"`
		TargetKey string `json:"target_key"`
	}
	if err := json.Unmarshal([]byte(approval.PayloadJSON), &payload); err != nil || payload.DocID == "" || payload.TargetKey == "" {
		return nil
	}
	return &v1.ApprovalTarget{
		Kind: "vector_gc_redrive", DocID: redact.Summary(payload.DocID, 64), TargetKey: redact.Summary(payload.TargetKey, 96),
	}
}

// ApprovalDecision submits an approval decision.
func (c *ControllerV1) ApprovalDecision(ctx context.Context, req *v1.ApprovalDecisionReq) (*v1.ApprovalDecisionRes, error) {
	if err := validateBoundedText(req.Comment, maxCommentRunes); err != nil {
		return nil, err
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)

	// Validate decision value
	if req.Decision != "approved" && req.Decision != "rejected" {
		return nil, apperr.New(40001, 400, "decision must be 'approved' or 'rejected'")
	}
	if err := c.app.ApprovalRepo.ExpireStale(ctx); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if req.Decision == "approved" {
		approval, err := c.app.ApprovalRepo.GetPending(ctx, tenantID, req.ApprovalID)
		if err != nil {
			return nil, apperr.Wrap(err, apperr.ErrInternal)
		}
		if approval == nil {
			return nil, apperr.New(40401, 404, "审批单不存在或已被处理")
		}
		// An approval is not an execution command. Every approval type that can
		// create a side effect must have its own atomic decision-and-resume
		// implementation (including requester/approver separation, immutable
		// input binding and idempotent execution). Do not fall through to the
		// generic status update: it would let the Portal claim "approved" while
		// no action is safely runnable. Rejections are terminal metadata-only
		// decisions and may use the generic CAS path below.
		if approvalRequiresDedicatedExecution(approval.ApprovalType) {
			return nil, apperr.ErrHighRiskWorkflowUnavailable
		}
		approved, err := c.app.VectorGCRepo.ApproveRedrive(ctx, tenantID, req.ApprovalID, userID)
		if err != nil {
			return nil, apperr.Wrap(err, apperr.ErrBadRequest)
		}
		if !approved {
			return nil, apperr.New(40401, 404, "审批单不存在、已过期或死信任务已变化")
		}
		return &v1.ApprovalDecisionRes{ApprovalID: req.ApprovalID, Status: req.Decision}, nil
	}

	updated, err := c.app.ApprovalRepo.Decide(ctx, tenantID, req.ApprovalID, req.Decision, userID, req.Comment)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if !updated {
		return nil, apperr.New(40401, 404, "审批单不存在或已被处理")
	}

	return &v1.ApprovalDecisionRes{
		ApprovalID: req.ApprovalID,
		Status:     req.Decision,
	}, nil
}

// approvalRequiresDedicatedExecution reports whether an approval type lacks
// an atomic decision-and-execution implementation. Add a new type to the
// allowlist only together with its durable command and resume worker.
func approvalRequiresDedicatedExecution(approvalType string) bool {
	return approvalType != repository.VectorGCRedriveApprovalType
}

// ListVectorGCTasks exposes tenant-scoped dead-letter diagnosis to platform
// administrators. It intentionally projects only redacted error summaries.
func (c *ControllerV1) ListVectorGCTasks(ctx context.Context, req *v1.ListVectorGCTasksReq) (*v1.ListVectorGCTasksRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	tasks, total, err := c.app.VectorGCRepo.List(ctx, tenantID, req.Status, req.Page, req.Size)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	items := make([]v1.VectorGCTaskItem, 0, len(tasks))
	for _, task := range tasks {
		nextAttempt := ""
		if task.NextAttemptAt != nil {
			nextAttempt = task.NextAttemptAt.UTC().Format(time.RFC3339)
		}
		items = append(items, v1.VectorGCTaskItem{
			DocID: task.DocID, TargetKey: task.TargetKey, TargetKind: task.TargetKind, TargetGeneration: task.TargetGeneration,
			Status: task.Status, AttemptCount: task.AttemptCount, MaxAttempts: task.MaxAttempts,
			LastError: redact.Summary(task.LastError, 1000), NextAttemptAt: nextAttempt,
		})
	}
	return &v1.ListVectorGCTasksRes{Items: items, Total: total}, nil
}

// RequestVectorGCRedrive creates a short-lived approval for one exact dead
// target. A different administrator must approve it before it can re-enter the
// worker queue, retaining tenant isolation and separation of duties.
func (c *ControllerV1) RequestVectorGCRedrive(ctx context.Context, req *v1.RequestVectorGCRedriveReq) (*v1.RequestVectorGCRedriveRes, error) {
	tenantID, userID := ctxkeys.TenantIDFrom(ctx), ctxkeys.UserIDFrom(ctx)
	payload, err := json.Marshal(map[string]string{
		"doc_id": req.DocID, "target_key": req.TargetKey, "requested_by": userID, "reason": redact.Summary(req.Reason, 500),
	})
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	approvalID := "gc-approval-" + uuid.NewString()
	resolvedID, _, err := c.app.VectorGCRepo.RequestRedriveApproval(ctx, tenantID, req.DocID, req.TargetKey, userID, approvalID, string(payload), time.Now().Add(15*time.Minute))
	if err != nil {
		// Redrive is an operator-facing API. Preserve only a stable business
		// error; SQL driver details (including "no rows") must never reach the
		// client or become a misleading 500 response.
		return nil, apperr.New(40002, 400, "向量清理任务不存在、不是死信或已不可重驱")
	}
	return &v1.RequestVectorGCRedriveRes{ApprovalID: resolvedID, Status: "pending"}, nil
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

// toOpsEvidence projects domain.Evidence into the API DTO.
func toOpsEvidence(in []domain.Evidence) []v1.OpsEvidence {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.OpsEvidence, 0, len(in))
	for _, e := range in {
		out = append(out, v1.OpsEvidence{
			ToolName:    e.ToolName,
			Source:      evidenceSource(e.ToolName),
			Status:      e.Status,
			LatencyMS:   e.LatencyMS,
			Timestamp:   e.Timestamp,
			Suppressed:  true,
			InputBytes:  len([]byte(e.Input)),
			OutputBytes: len([]byte(e.Output)),
		})
	}
	return out
}

func toSafeDetails(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, redact.TelemetryProjection(value))
	}
	return out
}

// safeOpsResult intentionally does not expose the agent's free-form result.
// It can embed raw tool output and is therefore unsuitable for the Portal or
// other ordinary API clients. The separately projected conclusion and tool
// state provide the supported diagnostic contract.
func safeOpsResult(status domain.OpsTaskStatus, query string) string {
	switch status {
	case domain.OpsTaskSuccess:
		if summary := opsDiagnosticSummary(query); summary != "" {
			return "诊断任务已完成（" + summary + "），请查看结构化结论和受抑制的工具状态。"
		}
		return "诊断任务已完成，请查看结构化结论和受抑制的工具状态。"
	case domain.OpsTaskPending, domain.OpsTaskRunning:
		return "诊断任务正在执行。"
	case domain.OpsTaskTimeout:
		return "诊断任务超时，请根据 trace ID 进一步排查。"
	default:
		if isInsufficientOpsContext(query) {
			return "当前信息不足，需要补充服务名称、异常接口、发生时间范围和相关指标。"
		}
		return "诊断任务未完成，请根据任务状态和 trace ID 排查。"
	}
}

// evidenceSource is a small allowlist of adapter provenance identifiers. It
// is metadata, not adapter output, so callers can evaluate/operate safely
// without exposing free text from logs, metrics or RAG documents.
func evidenceSource(toolName string) string {
	switch toolName {
	case "query_prometheus_alerts", "query_metric_range":
		return "prometheus"
	case "query_internal_docs":
		return "knowledge_base"
	case "search_logs", "query_logs", "query_logs_by_trace":
		return "logs"
	case "query_deployments":
		return "deployment_api"
	case "get_current_time", "mcp_time_get_current_time", "mcp_time_convert_time":
		return "time_service"
	default:
		return ""
	}
}

// opsDiagnosticCategory classifies a request into an allowlisted presentation
// label. It never returns the request body, entity identifiers, or model/tool
// text, but lets operators distinguish a completed alert/metric/log workflow.
func opsDiagnosticSummary(query string) string {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(query, "限流") || strings.Contains(lower, "rate limit"):
		return "诊断类别：限流；信号：rate limit triggered；影响范围与止血措施待结构化结论确认"
	case strings.Contains(query, "分布式锁") || strings.Contains(query, "锁异常") || strings.Contains(lower, "distributed lock"):
		return "诊断类别：分布式锁；信号：distributed lock wait timeout；影响范围、根因与止血待结构化结论确认"
	case strings.Contains(query, "告警") || strings.Contains(lower, "alert") || strings.Contains(lower, "firing"):
		return "诊断类别：告警"
	case strings.Contains(query, "指标") || strings.Contains(query, "错误率") || strings.Contains(query, "延迟") || strings.Contains(lower, "metric") || strings.Contains(lower, "latency"):
		return "诊断类别：指标"
	case strings.Contains(query, "日志") || strings.Contains(lower, "log") || strings.Contains(lower, "trace"):
		return "诊断类别：日志"
	case strings.Contains(query, "发布") || strings.Contains(query, "部署") || strings.Contains(query, "回滚") || strings.Contains(lower, "deploy") || strings.Contains(lower, "rollback"):
		return "诊断类别：发布变更"
	default:
		return ""
	}
}

func isInsufficientOpsContext(query string) bool {
	return domain.NeedsOpsClarification(query)
}

func (c *ControllerV1) GetTrace(ctx context.Context, req *v1.GetTraceReq) (*v1.GetTraceRes, error) {
	if c.app.TraceRepo == nil {
		return nil, apperr.ErrNotFound
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	if tenantID == "" {
		tenantID = domain.DefaultTenantID
	}
	trace, err := c.app.TraceRepo.Get(ctx, tenantID, req.TraceID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if trace == nil {
		return nil, apperr.ErrNotFound
	}
	if !canReadOwnedResource(ctx, trace.UserID) {
		return nil, apperr.ErrNotFound
	}
	steps, err := c.app.TraceRepo.ListSteps(ctx, tenantID, req.TraceID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	return &v1.GetTraceRes{
		Trace: toAgentTrace(trace),
		Steps: toAgentTraceSteps(steps),
	}, nil
}

func (c *ControllerV1) requireSessionRead(ctx context.Context, tenantID, sessionID string) error {
	if c.app.SessionRepo == nil {
		return apperr.ErrNotFound
	}
	session, err := c.app.SessionRepo.Get(ctx, tenantID, sessionID)
	if err != nil {
		return apperr.Wrap(err, apperr.ErrInternal)
	}
	if session == nil || !canReadOwnedResource(ctx, session.UserID) {
		return apperr.ErrNotFound
	}
	return nil
}

func canReadOwnedResource(ctx context.Context, ownerID string) bool {
	return ownerID != "" && (ownerID == ctxkeys.UserIDFrom(ctx) || canReadAllTenantResources(ctx))
}

func canReadAllTenantResources(ctx context.Context) bool {
	for _, role := range ctxkeys.RolesFrom(ctx) {
		if role == string(domain.RoleSREAdmin) || role == string(domain.RolePlatformAdmin) {
			return true
		}
	}
	return false
}

func toAgentTrace(in *repository.AgentTrace) *v1.AgentTrace {
	if in == nil {
		return nil
	}
	finishedAt := ""
	if in.FinishedAt != nil && !in.FinishedAt.IsZero() {
		finishedAt = in.FinishedAt.UTC().Format(time.RFC3339)
	}
	return &v1.AgentTrace{
		TraceID:    in.TraceID,
		TenantID:   in.TenantID,
		UserID:     in.UserID,
		AgentType:  in.AgentType,
		SessionID:  in.SessionID,
		TaskID:     in.TaskID,
		Query:      redact.Summary(in.Query, 4000),
		Status:     in.Status,
		LatencyMS:  in.LatencyMS,
		ErrorMsg:   redact.Summary(in.ErrorMsg, 2000),
		StartedAt:  in.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt: finishedAt,
	}
}

func toAgentTraceSteps(in []repository.AgentTraceStep) []v1.AgentTraceStep {
	out := make([]v1.AgentTraceStep, 0, len(in))
	for _, s := range in {
		createdAt := ""
		if !s.CreatedAt.IsZero() {
			createdAt = s.CreatedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, v1.AgentTraceStep{
			ID:            s.ID,
			StepType:      s.StepType,
			StepName:      s.StepName,
			InputSummary:  redact.Summary(s.InputSummary, 2000),
			OutputSummary: redact.Summary(s.OutputSummary, 4000),
			Status:        s.Status,
			LatencyMS:     s.LatencyMS,
			ErrorMsg:      redact.Summary(s.ErrorMsg, 2000),
			CreatedAt:     createdAt,
		})
	}
	return out
}

func toOpsTiming(in *domain.OpsTiming) *v1.OpsTiming {
	if in == nil {
		return nil
	}
	return &v1.OpsTiming{
		QueueDurationMS: in.QueueDurationMS,
		RunDurationMS:   in.RunDurationMS,
		E2EDurationMS:   in.E2EDurationMS,
		CreatedAt:       in.CreatedAt,
		StartedAt:       in.StartedAt,
		FinishedAt:      in.FinishedAt,
	}
}

// toOpsConclusion projects domain.FaultConclusion into the API DTO.
func toOpsConclusion(in *domain.FaultConclusion) *v1.OpsFaultConclusion {
	if in == nil {
		return nil
	}
	return &v1.OpsFaultConclusion{
		Symptom:     redact.Summary(in.Symptom, 1000),
		Impact:      redact.Summary(in.Impact, 1000),
		RootCause:   redact.Summary(in.RootCause, 2000),
		Workaround:  redact.Summary(in.Workaround, 2000),
		Remediation: redact.Summary(in.Remediation, 2000),
		Confidence:  in.Confidence,
		Source:      in.Source,
	}
}
