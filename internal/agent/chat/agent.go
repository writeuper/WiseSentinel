// Package chat implements the Chat Agent using Eino's ReAct framework.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/pkg/trace"
	"wisesentinel-platform/internal/repository"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

const maxStep = 25

const (
	defaultChatRAGTopK = 3
	runbookRAGTopK     = 8
)

const (
	ragStepSuccess = "success"
	ragStepEmpty   = "empty"
	ragStepSkipped = "skipped"
	ragStepError   = "error"
)

// Agent implements domain.ChatAgent using Eino's ReAct agent.
type Agent struct {
	modelRouter domain.ModelRouter
	ragService  domain.RAGService
	toolGateway domain.ToolGateway
	traceRepo   *repository.AgentTraceRepo
}

// NewAgent creates a chat agent.
func NewAgent(modelRouter domain.ModelRouter, ragService domain.RAGService, toolGateway domain.ToolGateway, traceRepo ...*repository.AgentTraceRepo) *Agent {
	agent := &Agent{
		modelRouter: modelRouter,
		ragService:  ragService,
		toolGateway: toolGateway,
	}
	if len(traceRepo) > 0 {
		agent.traceRepo = traceRepo[0]
	}
	return agent
}

// Invoke processes a synchronous chat request.
func isCurrentTimeQuery(query string) bool {
	lower := strings.ToLower(query)
	return strings.Contains(query, "北京时间") || strings.Contains(query, "当前时间") || strings.Contains(query, "现在几点") || strings.Contains(lower, "current time")
}

func isTimeConversionQuery(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return strings.Contains(q, "转换") || strings.Contains(q, "时区") || strings.Contains(q, "跨区") || strings.Contains(q, "utc") || strings.Contains(q, "东部")
}

func timeConversionTarget(query string) string {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "美国东部") || strings.Contains(q, "东部") || strings.Contains(q, "eastern"):
		return "America/New_York"
	case strings.Contains(q, "utc") && !strings.Contains(q, "utc+8"):
		return "UTC"
	default:
		return "Asia/Shanghai"
	}
}

func (a *Agent) timeConversionEvidence(ctx context.Context, req *domain.ChatAgentRequest) (*domain.ToolInvokeResponse, error) {
	if a.toolGateway == nil {
		return nil, fmt.Errorf("tool gateway is unavailable")
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	conversionTime := time.Now().In(mustLoadShanghai()).Format("2006-01-02 15:04:05")
	if explicit := extractConversionTime(req.Query); explicit != "" {
		conversionTime = explicit
	}
	input, err := json.Marshal(map[string]string{
		"source_timezone": "Asia/Shanghai",
		"time":            conversionTime,
		"target_timezone": timeConversionTarget(req.Query),
	})
	if err != nil {
		return nil, err
	}
	return a.toolGateway.Invoke(ctx, &domain.ToolInvokeRequest{
		TenantID: tenantID, UserID: ctxkeys.UserIDFrom(ctx), TraceID: ctxkeys.TraceIDFrom(ctx),
		ToolName: "mcp_time_convert_time", Input: input, AgentType: domain.AgentTypeChat,
	})
}

func mustLoadShanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return loc
}

var conversionTimePattern = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(?:[ T]\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:?\d{2})?)?\b`)

func extractConversionTime(query string) string {
	match := conversionTimePattern.FindString(query)
	if match == "" || len(match) == len("2006-01-02") {
		return ""
	}
	return strings.Replace(match, "T", " ", 1)
}

func formatConvertedTimeAnswer(output string) string {
	var evidence struct {
		SourceTimezone string `json:"source_timezone"`
		TargetTimezone string `json:"target_timezone"`
		SourceTime     string `json:"source_time"`
		Time           string `json:"time"`
		RFC3339        string `json:"rfc3339"`
	}
	if err := json.Unmarshal([]byte(output), &evidence); err != nil || evidence.Time == "" {
		return output
	}
	return fmt.Sprintf("时间转换结果：%s（%s）→ %s（%s）。RFC3339：%s。证据来源：local time conversion service（source: local）。", evidence.SourceTime, evidence.SourceTimezone, evidence.Time, evidence.TargetTimezone, evidence.RFC3339)
}

// isPlatformRoleQuery identifies questions about the platform's configured
// collaborating Agent roles. These are product metadata questions and should
// not be answered from operational Runbook retrieval results.
func isPlatformRoleQuery(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return strings.Contains(q, "agent角色") ||
		strings.Contains(q, "agent 角色") ||
		(strings.Contains(q, "角色") && (strings.Contains(q, "协作") || strings.Contains(q, "有哪些") || strings.Contains(q, "支持")))
}

func platformCapabilityAnswer(query string) (string, string, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(q, "平均执行步数") || strings.Contains(q, "平均步数") || (strings.Contains(q, "执行步数") && strings.Contains(q, "查看")):
		return "平台通过持久化 Trace Step 统计 Agent 执行步数。质量聚合报告会输出平均 Trace 步数、最小值和最大值；也可以在授权的 Trace 详情中查看单次任务的步骤明细。步数统计包含路由、RAG、模型、Planner、Executor、Replanner 和工具步骤，不应把单次工具调用数直接当成 Agent 步数。", "static_agent_step_metrics", true
	case strings.Contains(q, "超时") && (strings.Contains(q, "模型") || strings.Contains(q, "降级") || strings.Contains(q, "重试")):
		return "平台对模型服务超时采用统一超时预算、有限次数重试、指数退避和错误分类；重试仅针对可恢复的 5xx/网络错误，超时会停止继续重试并返回稳定的超时错误。模型配置按 profile 共享准入和熔断状态，避免重试放大或绕过容量保护。", "static_platform_model_resilience", true
	case strings.Contains(q, "质量报告") && (strings.Contains(q, "模型原文") || strings.Contains(q, "脱敏") || strings.Contains(q, "导出")):
		return "质量报告默认只输出聚合指标和脱敏后的分类信息，不包含用户问题原文、模型原文、工具参数、凭证、Token、DSN 或完整 Trace 内容。报告可统计任务完成率、工具成功率、延迟分位数、RAG 检索质量和失败分类；如需审计详情，应通过授权的 Trace/审计接口查看脱敏摘要。", "static_quality_report_projection", true
	case strings.Contains(q, "模型输出") && (strings.Contains(q, "脱敏") || strings.Contains(q, "敏感信息")):
		return "平台在模型输出进入会话、SSE、Trace、工具审计和质量报告前执行分层脱敏：凭证、Token、DSN、密码和疑似 PII 只保留受控摘要或分类；原始模型内容不进入运营指标和普通审计投影。需要审计时通过授权接口查看最小化摘要，并以 Trace ID 关联，不在前端直接展示敏感原文。", "static_model_output_redaction", true
	case strings.Contains(q, "评测") && (strings.Contains(q, "case") || strings.Contains(q, "用例")) && (strings.Contains(q, "配置") || strings.Contains(q, "创建") || strings.Contains(q, "新增")):
		return "新增 Agent 评测 Case 时，建议为每条 Case 配置唯一 case_id、场景和输入，明确 expected_route、expected_tools、forbidden_tools、expected_source、expected_knowledge 与 expected_keywords，并为通过条件设置可审计断言。提交前先在隔离集成租户执行单条回归，再纳入 100+ 冻结评测集；工具断言必须以持久化 Trace Step 为准，不能只匹配模型文本。", "static_eval_case_configuration", true
	default:
		return "", "", false
	}
}

func platformRoleAnswer() string {
	return "平台当前支持以下 Agent 角色协作：资深 Agent 架构师、资深 Golang 后端开发工程师、资深前端工程师、自动化测试工程师。架构师负责方案与边界设计，后端和前端负责实现，测试工程师负责自动化验证、评测与回归修复；实际可用角色以租户配置和权限为准。"
}

func (a *Agent) currentTimeEvidence(ctx context.Context, req *domain.ChatAgentRequest) (*domain.ToolInvokeResponse, error) {
	if a.toolGateway == nil {
		return nil, fmt.Errorf("tool gateway is unavailable")
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	return a.toolGateway.Invoke(ctx, &domain.ToolInvokeRequest{
		TenantID:  tenantID,
		UserID:    ctxkeys.UserIDFrom(ctx),
		TraceID:   ctxkeys.TraceIDFrom(ctx),
		ToolName:  "get_current_time",
		Input:     []byte(`{}`),
		AgentType: domain.AgentTypeChat,
	})
}

func formatCurrentTimeAnswer(output string) string {
	var evidence struct {
		Time     string `json:"time"`
		Timezone string `json:"timezone"`
		RFC3339  string `json:"rfc3339"`
	}
	if err := json.Unmarshal([]byte(output), &evidence); err != nil {
		return output
	}
	location, err := time.LoadLocation(evidence.Timezone)
	if err != nil {
		return output
	}
	current, err := time.ParseInLocation("2006-01-02 15:04:05", evidence.Time, location)
	if err != nil && evidence.RFC3339 != "" {
		current, err = time.Parse(time.RFC3339, evidence.RFC3339)
		if err == nil {
			current = current.In(location)
		}
	}
	if err != nil {
		return output
	}
	windowStart := current.Add(-30 * time.Minute)
	currentText := current.In(location).Format("2006-01-02 15:04:05")
	windowStartText := windowStart.In(location).Format("2006-01-02 15:04:05")
	return fmt.Sprintf("当前北京时间（%s）：%s。建议排查窗口：最近30分钟，即 %s 至 %s（北京时间）。证据来源：local 时间服务。", evidence.Timezone, currentText, windowStartText, currentText)
}

func (a *Agent) Invoke(ctx context.Context, req *domain.ChatAgentRequest) (*domain.ChatAgentResponse, error) {
	ctx = ctxkeys.WithRequestQuery(ctx, req.Query)
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}
	startedAt := time.Now()
	a.startTrace(ctx, traceID, req, startedAt)
	ctx = ctxkeys.WithStepSink(ctx, func(stepType, stepName, input, output, status string, latencyMS int64, errMsg string) {
		a.recordStep(ctx, traceID, req, stepType, stepName, input, output, status, latencyMS, errMsg)
	})
	finishStatus := "success"
	finishErr := ""
	defer func() {
		a.finishTrace(ctx, traceID, finishStatus, finishErr, time.Since(startedAt).Milliseconds())
	}()

	if isTimeConversionQuery(req.Query) {
		toolResult, err := a.timeConversionEvidence(ctx, req)
		if err != nil {
			return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
		}
		answer := formatConvertedTimeAnswer(toolResult.Output)
		return &domain.ChatAgentResponse{SessionID: req.SessionID, Answer: answer, ToolCalls: []domain.ToolCallSummary{{Tool: "mcp_time_convert_time", Status: toolResult.Status, LatencyMS: toolResult.LatencyMS}}, TraceID: traceID}, nil
	}
	if isCurrentTimeQuery(req.Query) {
		toolResult, err := a.currentTimeEvidence(ctx, req)
		if err != nil {
			return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
		}
		answer := formatCurrentTimeAnswer(toolResult.Output)
		return &domain.ChatAgentResponse{SessionID: req.SessionID, Answer: answer, ToolCalls: []domain.ToolCallSummary{{Tool: "get_current_time", Status: toolResult.Status, LatencyMS: toolResult.LatencyMS}}, TraceID: traceID}, nil
	}
	if isPlatformRoleQuery(req.Query) {
		answer := platformRoleAnswer()
		a.recordStep(ctx, traceID, req, "capability", "static_platform_roles", req.Query, answer, "success", 0, "")
		return &domain.ChatAgentResponse{SessionID: req.SessionID, Answer: answer, TraceID: traceID}, nil
	}
	if answer, stepName, ok := platformCapabilityAnswer(req.Query); ok {
		a.recordStep(ctx, traceID, req, "capability", stepName, req.Query, answer, "success", 0, "")
		return &domain.ChatAgentResponse{SessionID: req.SessionID, Answer: answer, TraceID: traceID}, nil
	}

	// 1. Retrieve RAG documents and collect citations when enabled
	ragStart := time.Now()
	documents, citations, ragStatus, ragErr := a.retrieveDocs(ctx, req)
	a.recordStep(ctx, traceID, req, "rag", "retrieve", req.Query, documents, ragStatus, time.Since(ragStart).Milliseconds(), ragErr)

	// 2. Build the ReAct agent
	reactAgent, err := a.buildReActAgent(ctx, req, documents, traceID)
	if err != nil {
		finishStatus = "failed"
		finishErr = err.Error()
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	// 3. Build input messages: history + user query
	input := a.buildInputMessages(ctx, req)

	// 4. Generate response
	modelStart := time.Now()
	result, err := reactAgent.Generate(ctx, input)
	if err != nil {
		finishStatus = "failed"
		finishErr = err.Error()
		a.recordStep(ctx, traceID, req, "model", "generate", req.Query, "", "error", time.Since(modelStart).Milliseconds(), err.Error())
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, apperr.ErrModelTimeout
		}
		if isModelOverloadedError(err) {
			return nil, apperr.ErrModelOverloaded
		}
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}
	a.recordStep(ctx, traceID, req, "model", "generate", req.Query, result.Content, "success", time.Since(modelStart).Milliseconds(), "")

	return &domain.ChatAgentResponse{
		SessionID: req.SessionID,
		Answer:    result.Content,
		Citations: citations,
		ToolCalls: extractToolCallSummary(result),
		TraceID:   traceID,
	}, nil
}

// Eino's ReAct executor may format a child model error into NodeRunError
// without preserving Unwrap. Keep the public overload contract typed at the
// Agent boundary, matching the exact stable AppError message only.
func isModelOverloadedError(err error) bool {
	return errors.Is(err, apperr.ErrModelOverloaded) ||
		(err != nil && strings.Contains(err.Error(), apperr.ErrModelOverloaded.Message))
}

// Stream processes a streaming chat request and returns a StreamReader.
func (a *Agent) Stream(ctx context.Context, req *domain.ChatAgentRequest) (domain.StreamReader, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	// Create a cancellable context so Close() can terminate the goroutine.
	ctx, cancel := context.WithCancel(ctx)
	r := &chatStreamReader{
		events: make(chan streamEventItem, 64),
		cancel: cancel,
		done:   ctx.Done(),
	}

	go func() {
		defer close(r.events)
		defer cancel()
		startedAt := time.Now()
		a.startTrace(ctx, traceID, req, startedAt)
		streamStatus := "success"
		streamErr := ""
		defer func() {
			a.finishTrace(ctx, traceID, streamStatus, streamErr, time.Since(startedAt).Milliseconds())
		}()
		ctx = ctxkeys.WithStepSink(ctx, func(stepType, stepName, input, output, status string, latencyMS int64, errMsg string) {
			a.recordStep(ctx, traceID, req, stepType, stepName, input, output, status, latencyMS, errMsg)
		})

		// Check if context is already cancelled.
		select {
		case <-ctx.Done():
			streamStatus = "canceled"
			streamErr = ctx.Err().Error()
			return
		default:
		}

		r.send("connected", fmt.Sprintf(`{"status":"connected","session_id":"%s"}`, req.SessionID))

		if isTimeConversionQuery(req.Query) {
			toolResult, toolErr := a.timeConversionEvidence(ctx, req)
			if toolErr != nil {
				streamStatus = "failed"
				streamErr = toolErr.Error()
				r.send("error", "时间转换服务暂时不可用，请稍后重试。")
				r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
				return
			}
			toolPayload, _ := json.Marshal(map[string]any{"tool": "mcp_time_convert_time", "status": toolResult.Status, "latency_ms": toolResult.LatencyMS, "output": toolResult.Output})
			r.send("tool", string(toolPayload))
			r.send("message", formatConvertedTimeAnswer(toolResult.Output))
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}
		if isCurrentTimeQuery(req.Query) {
			toolResult, toolErr := a.currentTimeEvidence(ctx, req)
			if toolErr != nil {
				streamStatus = "failed"
				streamErr = toolErr.Error()
				g.Log().Errorf(ctx, "ChatAgent.Stream current-time tool failed trace_id=%s", traceID)
				r.send("error", "时间服务暂时不可用，请稍后重试。")
				r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
				return
			}
			toolPayload, _ := json.Marshal(map[string]any{
				"tool":       "get_current_time",
				"status":     toolResult.Status,
				"latency_ms": toolResult.LatencyMS,
				"output":     toolResult.Output,
			})
			r.send("tool", string(toolPayload))
			answer := formatCurrentTimeAnswer(toolResult.Output)
			r.send("message", answer)
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}
		if isPlatformRoleQuery(req.Query) {
			answer := platformRoleAnswer()
			a.recordStep(ctx, traceID, req, "capability", "static_platform_roles", req.Query, answer, "success", 0, "")
			r.send("message", answer)
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}
		if answer, stepName, ok := platformCapabilityAnswer(req.Query); ok {
			a.recordStep(ctx, traceID, req, "capability", stepName, req.Query, answer, "success", 0, "")
			r.send("message", answer)
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}

		// 1. Retrieve RAG docs. Citations are emitted before model text so the
		// client can show the evidence boundary even if generation later fails.
		ragStart := time.Now()
		documents, citations, ragStatus, ragErr := a.retrieveDocs(ctx, req)
		a.recordStep(ctx, traceID, req, "rag", "retrieve", req.Query, documents, ragStatus, time.Since(ragStart).Milliseconds(), ragErr)
		for _, citation := range citations {
			payload, marshalErr := json.Marshal(citation)
			if marshalErr != nil {
				g.Log().Warningf(ctx, "ChatAgent.Stream citation serialization failed trace_id=%s", traceID)
				continue
			}
			if !r.send("citation", string(payload)) {
				return
			}
		}

		// 2. Build the ReAct agent
		reactAgent, err := a.buildReActAgent(ctx, req, documents, traceID)
		if err != nil {
			streamStatus = "failed"
			streamErr = err.Error()
			g.Log().Errorf(ctx, "ChatAgent.Stream build agent failed trace_id=%s", traceID)
			r.send("error", "服务初始化失败，请稍后重试。")
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}

		// 3. Build input messages
		input := a.buildInputMessages(ctx, req)

		// 4. Stream response
		sr, err := reactAgent.Stream(ctx, input)
		if err != nil {
			streamStatus = "failed"
			streamErr = err.Error()
			g.Log().Errorf(ctx, "ChatAgent.Stream provider stream failed trace_id=%s", traceID)
			r.send("error", streamErrorData(err, "模型服务暂时不可用，请稍后重试。"))
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}
		defer sr.Close()

		// Use a separate goroutine for the blocking Recv so we can
		// select on ctx.Done() for cancellation.
		msgCh := make(chan *schema.Message, 8)
		errCh := make(chan error, 1)
		go func() {
			defer func() {
				close(msgCh)
				close(errCh)
			}()
			for {
				msg, recvErr := sr.Recv()
				if recvErr != nil {
					errCh <- recvErr
					return
				}
				select {
				case msgCh <- msg:
				case <-ctx.Done():
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				streamStatus = "canceled"
				streamErr = ctx.Err().Error()
				return
			case msg, ok := <-msgCh:
				if !ok {
					// A provider EOF is normal completion. Any other receive error
					// must be visible to the client and must not look like a clean
					// completed answer to the SSE handler.
					recvErr, received := <-errCh
					if received && recvErr != nil && !errors.Is(recvErr, io.EOF) {
						streamStatus = "failed"
						streamErr = recvErr.Error()
						g.Log().Errorf(ctx, "ChatAgent.Stream provider recv failed trace_id=%s", traceID)
						r.send("error", streamErrorData(recvErr, "模型流式响应中断，请稍后重试。"))
					}
					r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
					return
				}
				if msg.Content != "" {
					r.send("message", msg.Content)
				}
			}
		}
	}()

	return r, nil
}

// streamErrorData carries only a known retryable error code across an already
// committed SSE response. Arbitrary provider errors remain a stable message
// and must never enter the browser stream.
func streamErrorData(err error, fallback string) string {
	if !isModelOverloadedError(err) {
		return fallback
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"code":                apperr.ErrModelOverloaded.Code,
		"message":             apperr.ErrModelOverloaded.Message,
		"retry_after_seconds": 2,
	})
	if marshalErr != nil {
		return apperr.ErrModelOverloaded.Message
	}
	return string(payload)
}

// streamEventItem holds a single SSE-like event.
type streamEventItem struct {
	event string
	data  string
}

// chatStreamReader implements domain.StreamReader over a channel.
type chatStreamReader struct {
	events chan streamEventItem
	cancel context.CancelFunc
	done   <-chan struct{}
}

// send applies backpressure rather than silently discarding stream events.
// Closing the reader unblocks a producer that is waiting for a slow client.
func (r *chatStreamReader) send(event, data string) bool {
	select {
	case r.events <- streamEventItem{event: event, data: data}:
		return true
	case <-r.done:
		return false
	}
}

// Next returns the next event (event type, data payload, and whether more events exist).
func (r *chatStreamReader) Next() (event string, data string, ok bool) {
	item, more := <-r.events
	if !more {
		return "", "", false
	}
	return item.event, item.data, true
}

// Close cancels the stream context, terminating the background goroutine.
func (r *chatStreamReader) Close() error {
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

// buildReActAgent creates a new ReAct agent with the appropriate configuration.
func (a *Agent) buildReActAgent(ctx context.Context, req *domain.ChatAgentRequest, documents string, traceID string) (*react.Agent, error) {
	// 1. Get the chat model
	rawModel, err := a.modelRouter.ChatModel(ctx, domain.ModelProfileChatFast)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}
	chatModel, ok := rawModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, apperr.New(50002, 500, "model does not implement ToolCallingChatModel")
	}

	// 2. Get tools for the chat agent
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	einoTools := make([]tool.BaseTool, 0)
	if req.Options.EnableTools && a.toolGateway != nil {
		type einoToolLister interface {
			AsEinoTools(ctx context.Context, tenantID string, agentType domain.AgentType) ([]tool.BaseTool, error)
		}
		if lister, ok := a.toolGateway.(einoToolLister); ok {
			einoTools, err = lister.AsEinoTools(ctx, tenantID, domain.AgentTypeChat)
			if err != nil {
				return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
			}
		}
	}

	// 3. Build system prompt using ChatTemplate
	chatTemplate := NewChatTemplate(documents)

	// 4. Create ReAct agent config with the extracted MessageModifier
	config := &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: einoTools,
		},
		MessageModifier: chatTemplate.MessageModifier(),
		MaxStep:         maxStep,
		GraphName:       "ChatAgent",
	}

	agent, err := react.NewAgent(ctx, config)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	return agent, nil
}

// buildInputMessages builds the []*schema.Message from the user request.
func (a *Agent) buildInputMessages(ctx context.Context, req *domain.ChatAgentRequest) []*schema.Message {
	limits := contextLimits{
		historyTokens:    readContextInt(ctx, "chat.history_token_budget", defaultHistoryTokenBudget),
		messageCharLimit: readContextInt(ctx, "chat.history_message_char_limit", defaultMessageCharLimit),
	}
	history := compactHistory(req.History, req.Query, limits)
	messages := make([]*schema.Message, 0, len(history)+1)

	for _, h := range history {
		messages = append(messages, &schema.Message{
			Role:    schema.RoleType(h.Role),
			Content: h.Content,
		})
	}

	messages = append(messages, schema.UserMessage(req.Query))
	return messages
}

// retrieveDocs returns a safe prompt projection together with an explicit
// trace status. "empty" means that retrieval completed but did not provide
// usable evidence; "error" means the RAG dependency failed; "skipped" means
// the caller opted out. This distinction is essential for retrieval quality
// evaluation and long-tail diagnosis.
func (a *Agent) retrieveDocs(ctx context.Context, req *domain.ChatAgentRequest) (string, []domain.Citation, string, string) {
	documents := ""
	citations := make([]domain.Citation, 0, 3)

	if req == nil || !req.Options.EnableRAG {
		return documents, citations, ragStepSkipped, ""
	}
	if a.ragService == nil {
		return "RAG状态：不可用，以下回答不得视为内部知识库结论。", citations, ragStepError, "rag service unavailable"
	}
	// Explicit Runbook/document requests must execute query_internal_docs so
	// the persisted tool step, authorization boundary and tool evidence remain
	// auditable. Preloading the same chunks here lets the model answer without
	// invoking the tool, which makes the result look successful while losing
	// the required evidence chain. The adapter uses the expanded Runbook TopK.
	if req.Options.EnableTools && chatRAGTopK(req.Query) > defaultChatRAGTopK {
		return a.retrieveExplicitRunbook(ctx, req)
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	decision, err := a.ragService.Route(ctx, &domain.RetrieveRequest{
		TenantID: tenantID,
		Query:    req.Query,
		TopK:     chatRAGTopK(req.Query),
	})
	if err != nil || decision == nil || decision.Response == nil {
		errText := ""
		if err != nil {
			errText = redact.Summary(err.Error(), 500)
		}
		g.Log().Warningf(ctx, "RAG retrieval failed: query=%q err=%s decision_nil=%t response_nil=%t", redact.Summary(req.Query, 200), errText, decision == nil, decision != nil && decision.Response == nil)
		return "RAG状态：检索失败，以下回答不得视为内部知识库结论。", citations, ragStepError, errText
	}
	if decision.Confidence == domain.ConfidenceLow || len(decision.Response.Documents) == 0 {
		return "RAG状态：未找到足够相关的内部知识，以下回答不得视为内部知识库结论。", citations, ragStepEmpty, ""
	}
	ragResp := decision.Response
	if decision.Route == "realtime_tools" {
		documents += fmt.Sprintf("RAG路由: %s，置信度=%s，建议优先使用实时工具交叉验证。\n---\n", decision.Reason, decision.Confidence)
	}

	for _, doc := range ragResp.Documents {
		documents += fmt.Sprintf("来源[%s]: %s\n---\n", doc.Source, doc.Content)
		citations = append(citations, domain.Citation{
			DocID:   doc.DocID,
			ChunkID: doc.ChunkID,
			Source:  doc.Source,
			Snippet: truncate(doc.Content, 200),
			Version: doc.Version,
		})
	}

	return documents, citations, ragStepSuccess, ""
}

// retrieveExplicitRunbook executes the internal-docs tool synchronously for
// explicit Runbook/document requests. This closes a reliability gap where a
// model could answer from the preloaded RAG context without producing a
// persisted query_internal_docs tool step, making tool-hit evaluation and
// audit evidence inconsistent with the user-visible answer.
func (a *Agent) retrieveExplicitRunbook(ctx context.Context, req *domain.ChatAgentRequest) (string, []domain.Citation, string, string) {
	if a.toolGateway == nil {
		return "RAG状态：检索工具不可用，以下回答不得视为内部知识库结论。", nil, ragStepError, "tool gateway unavailable"
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	input, err := json.Marshal(map[string]string{"query": req.Query})
	if err != nil {
		return "RAG状态：检索请求构造失败。", nil, ragStepError, err.Error()
	}
	result, err := a.toolGateway.Invoke(ctx, &domain.ToolInvokeRequest{
		TenantID: tenantID, UserID: ctxkeys.UserIDFrom(ctx), TraceID: ctxkeys.TraceIDFrom(ctx),
		ToolName: "query_internal_docs", Input: input, AgentType: domain.AgentTypeChat,
	})
	if err != nil || result == nil {
		errText := "internal docs tool failed"
		if err != nil {
			errText = redact.Summary(err.Error(), 500)
		}
		return "RAG状态：检索失败，以下回答不得视为内部知识库结论。", nil, ragStepError, errText
	}
	var response domain.RetrieveResponse
	if err := json.Unmarshal([]byte(result.Output), &response); err != nil {
		return "RAG状态：检索结果格式无效，以下回答不得视为内部知识库结论。", nil, ragStepError, "invalid internal docs response"
	}
	if result.Status != "success" || len(response.Documents) == 0 || response.Confidence == domain.ConfidenceLow {
		return "RAG状态：未找到足够相关的内部知识，以下回答不得视为内部知识库结论。", nil, ragStepEmpty, ""
	}
	return renderRetrievedDocuments(response), citationsFromDocuments(response.Documents), ragStepSuccess, ""
}

func renderRetrievedDocuments(response domain.RetrieveResponse) string {
	documents := ""
	if response.Confidence != "" {
		documents = fmt.Sprintf("RAG路由: internal_docs_tool，置信度=%s。\n---\n", response.Confidence)
	}
	for _, doc := range response.Documents {
		documents += fmt.Sprintf("来源[%s]: %s\n---\n", doc.Source, doc.Content)
	}
	return documents
}

func citationsFromDocuments(documents []domain.RetrievedDocument) []domain.Citation {
	citations := make([]domain.Citation, 0, len(documents))
	for _, doc := range documents {
		citations = append(citations, domain.Citation{DocID: doc.DocID, ChunkID: doc.ChunkID, Source: doc.Source, Snippet: truncate(doc.Content, 200), Version: doc.Version})
	}
	return citations
}

// chatRAGTopK keeps ordinary questions inexpensive while preserving enough
// section-level context for explicit Runbook requests. Markdown Runbooks are
// intentionally split by headings; a top-three search can return only the
// title, scope and escalation sections while omitting the requested action
// sequence and mitigation. The higher bound is limited to explicit document
// requests so unrelated Chat traffic does not pay the larger retrieval cost.
func chatRAGTopK(query string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(q, "runbook") || strings.Contains(q, "排查手册") ||
		strings.Contains(q, "内部文档") || strings.Contains(q, "知识库") ||
		(strings.Contains(q, "检查顺序") && (strings.Contains(q, "止血") || strings.Contains(q, "升级"))) {
		return runbookRAGTopK
	}
	return defaultChatRAGTopK
}

func (a *Agent) startTrace(ctx context.Context, traceID string, req *domain.ChatAgentRequest, startedAt time.Time) {
	if a.traceRepo == nil {
		return
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	userID := req.UserID
	if userID == "" {
		userID = ctxkeys.UserIDFrom(ctx)
	}
	_ = a.traceRepo.Start(ctx, &repository.AgentTrace{
		TraceID:   traceID,
		TenantID:  tenantID,
		UserID:    userID,
		AgentType: string(domain.AgentTypeChat),
		SessionID: req.SessionID,
		Query:     req.Query,
		StartedAt: startedAt,
	})
}

func (a *Agent) finishTrace(ctx context.Context, traceID, status, errMsg string, latencyMS int64) {
	if a.traceRepo == nil {
		return
	}
	// Stream cancellation and model timeout can cancel the request context
	// before the deferred terminal write runs. Preserve trace identity values
	// but detach persistence from that cancellation so a running Trace cannot be
	// stranded forever and distort completion/latency aggregates.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = a.traceRepo.Finish(persistCtx, traceID, status, errMsg, latencyMS)
}

func (a *Agent) recordStep(ctx context.Context, traceID string, req *domain.ChatAgentRequest, stepType, stepName, input, output, status string, latencyMS int64, errMsg string) {
	if a.traceRepo == nil || traceID == "" {
		return
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	_ = a.traceRepo.AddStep(ctx, &repository.AgentTraceStep{
		TraceID:       traceID,
		TenantID:      tenantID,
		AgentType:     string(domain.AgentTypeChat),
		StepType:      stepType,
		StepName:      stepName,
		InputSummary:  truncate(input, 1000),
		OutputSummary: truncate(output, 2000),
		Status:        status,
		LatencyMS:     latencyMS,
		ErrorMsg:      truncate(errMsg, 1000),
	})
}

func extractToolCallSummary(msg *schema.Message) []domain.ToolCallSummary {
	if len(msg.ToolCalls) == 0 {
		return nil
	}
	summary := make([]domain.ToolCallSummary, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		summary[i] = domain.ToolCallSummary{
			Tool:   tc.Function.Name,
			Status: "success",
		}
	}
	return summary
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// Ensure consistent import
var _ = model.WithTemperature
