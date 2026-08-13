// Package ops implements the Ops Agent using Eino v0.6.0's Plan-Execute-Replan pattern.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/pkg/trace"
	"wisesentinel-platform/internal/repository"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// Agent implements domain.OpsAgent.
//
// It wires together the Planner, Executor, and Replanner produced by the
// prebuilt planexecute package and runs the resulting ADK agent against
// the configured models and tools.
type Agent struct {
	modelRouter   domain.ModelRouter
	toolGateway   domain.ToolGateway
	taskRepo      *repository.OpsTaskRepo
	traceRepo     *repository.AgentTraceRepo
	knowledgeRepo *repository.FaultKnowledgeRepo
	maxIter       int
}

// NewAgent creates a new Ops Agent.
func NewAgent(modelRouter domain.ModelRouter, toolGateway domain.ToolGateway, taskRepo *repository.OpsTaskRepo, knowledgeRepo *repository.FaultKnowledgeRepo, traceRepo ...*repository.AgentTraceRepo) *Agent {
	agent := &Agent{
		modelRouter:   modelRouter,
		toolGateway:   toolGateway,
		taskRepo:      taskRepo,
		knowledgeRepo: knowledgeRepo,
		maxIter:       20, // matches design doc §6.2
	}
	if len(traceRepo) > 0 {
		agent.traceRepo = traceRepo[0]
	}
	return agent
}

// Analyze runs the Ops agent for the given request.
//
//   - sync mode: runs the agent and returns the final result inline.
//   - async mode: creates a row in ws_ops_task, kicks off a background run,
//     and returns immediately with status=pending.
func clarificationForQuery(query string) string {
	if domain.NeedsOpsClarification(query) {
		return "当前信息不足，需要补充服务名称、异常接口或错误现象、发生时间范围和相关指标；例如：order-service 最近 15 分钟错误率升高，请查询日志和指标。"
	}
	return ""
}

func (a *Agent) Analyze(ctx context.Context, req *domain.OpsAgentRequest) (*domain.OpsAgentResponse, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		taskID = "ops_" + uuid.NewString()
	}
	userID := ctxkeys.UserIDFrom(ctx)
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}

	// Signed Alertmanager deliveries already carry a bounded, projected alert
	// payload. They are admitted as an operational signal even when the alert
	// name does not match the interactive service-id heuristic; the webhook
	// route still constrains the prompt and tool policy.
	if req.TriggerType != "webhook" {
		if clarification := clarificationForQuery(req.Query); clarification != "" {
			return &domain.OpsAgentResponse{Status: domain.OpsTaskFailed, Result: clarification, TraceID: traceID}, nil
		}
	}

	// 1. Create the task row (pending).
	triggerType := req.TriggerType
	if triggerType == "" {
		triggerType = "manual"
	}
	task := &repository.OpsTask{
		TenantID:    tenantID,
		TaskID:      taskID,
		TriggerType: triggerType,
		InputQuery:  req.Query,
		Status:      string(domain.OpsTaskPending),
		TraceID:     traceID,
		CreatedBy:   userID,
		MaxRetry:    2,
	}
	if err := a.taskRepo.Create(ctx, task); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	// Async tasks are persisted as pending and claimed by OpsWorker.
	if req.Async {
		return &domain.OpsAgentResponse{
			TaskID:  taskID,
			Status:  domain.OpsTaskPending,
			TraceID: traceID,
		}, nil
	}

	// Sync requests still acquire an execution lease so their terminal update
	// cannot race an async worker or a stale retry.
	executionToken := uuid.NewString()
	claimed, err := a.taskRepo.ClaimRunnable(ctx, tenantID, taskID, executionToken, time.Now().Add(30*time.Minute))
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if !claimed {
		return nil, apperr.ErrAgentFailed
	}
	return a.executeTask(ctx, tenantID, taskID, traceID, executionToken, req, false)
}

// ExecuteTask executes an existing persisted task. Workers must use this
// method instead of Analyze so task execution never creates another task.
func (a *Agent) ExecuteTask(ctx context.Context, tenantID, taskID, executionToken string) (*domain.OpsAgentResponse, error) {
	if strings.TrimSpace(executionToken) == "" {
		return nil, apperr.ErrAgentFailed
	}
	task, err := a.taskRepo.Get(ctx, tenantID, taskID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if task == nil {
		return nil, apperr.ErrNotFound
	}
	if task.Status != string(domain.OpsTaskRunning) || task.ExecutionToken != executionToken {
		return nil, apperr.ErrAgentFailed
	}
	traceID := task.TraceID
	if traceID == "" {
		traceID = trace.NewID()
	}
	ctx = ctxkeys.WithTraceID(ctx, traceID)
	return a.executeTask(ctx, tenantID, taskID, traceID, executionToken, &domain.OpsAgentRequest{
		TenantID: tenantID,
		UserID:   task.CreatedBy,
		Query:    task.InputQuery,
		Async:    false,
	}, true)
}

func (a *Agent) executeTask(ctx context.Context, tenantID, taskID, traceID, executionToken string, req *domain.OpsAgentRequest, allowRetry bool) (*domain.OpsAgentResponse, error) {
	startedAt := time.Now()
	a.startTrace(ctx, traceID, tenantID, taskID, req, startedAt)
	finishStatus := "success"
	finishErr := ""
	defer func() {
		a.finishTrace(ctx, traceID, finishStatus, finishErr, time.Since(startedAt).Milliseconds())
	}()

	evidence, result, detail, err := a.runAgent(ctx, tenantID, req)
	if err != nil {
		finishStatus = "failed"
		finishErr = redact.Summary(err.Error(), 1000)
		payload := redact.JSON(marshalPayload(detail, evidence, nil))
		status := domain.OpsTaskFailed
		if errors.Is(err, context.DeadlineExceeded) {
			status = domain.OpsTaskTimeout
			finishStatus = "timeout"
		}
		if status == domain.OpsTaskTimeout || !allowRetry {
			_, _ = a.taskRepo.FinishIfOwned(ctx, tenantID, taskID, executionToken,
				string(status), finishErr, payload)
		} else if task, getErr := a.taskRepo.Get(ctx, tenantID, taskID); getErr == nil && task != nil {
			_, _, _ = a.taskRepo.RetryOrFailIfOwned(ctx, tenantID, taskID, executionToken,
				finishErr, time.Now().Add(repository.RetryBackoff(task.RetryCount)))
		}
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	conclusion := parseConclusion(result)
	payload := redact.JSON(marshalPayload(detail, evidence, conclusion))
	finished, persistErr := a.taskRepo.FinishIfOwned(ctx, tenantID, taskID, executionToken,
		string(domain.OpsTaskSuccess), redact.Summary(result, 8000), payload)
	if persistErr != nil {
		return nil, apperr.Wrap(persistErr, apperr.ErrInternal)
	}
	if !finished {
		return nil, apperr.ErrAgentFailed
	}
	a.createKnowledgeDraft(ctx, tenantID, req.UserID, taskID, traceID, req.Query, evidence, conclusion)
	task, _ := a.taskRepo.Get(ctx, tenantID, taskID)
	return &domain.OpsAgentResponse{
		TaskID:     taskID,
		Status:     domain.OpsTaskSuccess,
		Result:     result,
		Detail:     detail,
		TraceID:    traceID,
		Evidence:   evidence,
		Conclusion: conclusion,
		Timing:     buildOpsTiming(task),
	}, nil
}

// runAgent builds and runs the Plan-Execute-Replan graph.
func (a *Agent) runAgent(ctx context.Context, tenantID string, req *domain.OpsAgentRequest) ([]domain.Evidence, string, []string, error) {
	// Attach a tool-call sink so the Tool Gateway records every tool invocation
	// as structured evidence for the Portal proof chain.
	var evidence []domain.Evidence
	ctx = ctxkeys.WithToolSink(ctx, &evidence)
	ctx = ctxkeys.WithStepSink(ctx, a.stepSink(ctx, tenantID))
	ctx = ctxkeys.WithRequestQuery(ctx, req.Query)

	// 1. Build planner / executor / replanner.
	plannerAgent, err := NewPlanner(ctx, a.modelRouter)
	if err != nil {
		return nil, "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}
	executorAgent, err := NewExecutor(ctx, a.modelRouter, a.toolGateway, tenantID)
	if err != nil {
		return nil, "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}
	replannerAgent, err := NewReplanner(ctx, a.modelRouter)
	if err != nil {
		return nil, "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	// 2. Wire the plan-execute-replan agent.
	planExecuteAgent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       plannerAgent,
		Executor:      executorAgent,
		Replanner:     replannerAgent,
		MaxIterations: a.maxIter,
	})
	if err != nil {
		return nil, "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	// 3. Run via the ADK Runner.
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: planExecuteAgent})

	query := req.Query
	if strings.TrimSpace(query) == "" {
		query = defaultOpsQuery
	}
	if evidence, result, detail, handled, err := a.runFocusedTool(ctx, tenantID, query); handled {
		return evidence, result, detail, err
	}

	iter := runner.Query(ctx, query)
	var (
		result      string
		detail      []string
		lastMessage adk.Message
	)
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			errText := redact.Summary(event.Err.Error(), 1000)
			g.Log().Errorf(ctx, "ops agent event error: %s", errText)
			detail = append(detail, fmt.Sprintf("[error] %s", errText))
			a.recordStep(ctx, tenantID, classifyStepType(event.AgentName), event.AgentName, "", errText, "error", 0, errText)
			// A graph/model failure is not an evidence-free successful diagnosis.
			// In particular, preserve the stable overload error so callers can
			// distinguish capacity shedding from a completed Ops task.
			return evidence, "", detail, normalizeOpsEventError(event.Err)
		}
		if event.Output != nil {
			msg, _, mErr := adk.GetMessage(event)
			if mErr == nil && msg != nil {
				lastMessage = msg
				content := truncate(msg.Content, 400)
				detail = append(detail, fmt.Sprintf("[%s] %s", event.AgentName, content))
				a.recordStep(ctx, tenantID, classifyStepType(event.AgentName), event.AgentName, "", content, "success", 0, "")
			}
		}
	}

	if lastMessage == nil {
		fallbackResult := buildFallbackConclusion(req.Query, detail, evidence)
		detail = append(detail, "[fallback] Agent 未能产生最终文本输出，已基于已采集证据生成阶段一兜底结论")
		return evidence, fallbackResult, detail, nil
	}
	result = lastMessage.Content
	return evidence, result, detail, nil
}

func isModelOverloadedError(err error) bool {
	return errors.Is(err, apperr.ErrModelOverloaded) ||
		(err != nil && strings.Contains(err.Error(), apperr.ErrModelOverloaded.Message))
}

func normalizeOpsEventError(err error) error {
	if isModelOverloadedError(err) {
		return apperr.ErrModelOverloaded
	}
	return err
}

func (a *Agent) startTrace(ctx context.Context, traceID, tenantID, taskID string, req *domain.OpsAgentRequest, startedAt time.Time) {
	if a.traceRepo == nil {
		return
	}
	userID := req.UserID
	if userID == "" {
		userID = ctxkeys.UserIDFrom(ctx)
	}
	_ = a.traceRepo.Start(ctx, &repository.AgentTrace{
		TraceID:   traceID,
		TenantID:  tenantID,
		UserID:    userID,
		AgentType: string(domain.AgentTypeOps),
		TaskID:    taskID,
		Query:     req.Query,
		StartedAt: startedAt,
	})
}

func (a *Agent) finishTrace(ctx context.Context, traceID, status, errMsg string, latencyMS int64) {
	if a.traceRepo == nil {
		return
	}
	_ = a.traceRepo.Finish(ctx, traceID, status, errMsg, latencyMS)
}

func (a *Agent) stepSink(ctx context.Context, tenantID string) ctxkeys.StepSinkFunc {
	return func(stepType, stepName, input, output, status string, latencyMS int64, errMsg string) {
		a.recordStep(ctx, tenantID, stepType, stepName, input, output, status, latencyMS, errMsg)
	}
}

func (a *Agent) recordStep(ctx context.Context, tenantID, stepType, stepName, input, output, status string, latencyMS int64, errMsg string) {
	if a.traceRepo == nil {
		return
	}
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		return
	}
	_ = a.traceRepo.AddStep(ctx, &repository.AgentTraceStep{
		TraceID:       traceID,
		TenantID:      tenantID,
		AgentType:     string(domain.AgentTypeOps),
		StepType:      stepType,
		StepName:      stepName,
		InputSummary:  truncate(input, 1000),
		OutputSummary: truncate(output, 2000),
		Status:        status,
		LatencyMS:     latencyMS,
		ErrorMsg:      truncate(errMsg, 1000),
	})
}

func classifyStepType(agentName string) string {
	name := strings.ToLower(agentName)
	switch {
	case strings.Contains(name, "planner"):
		return "planner"
	case strings.Contains(name, "replanner"):
		return "replanner"
	case strings.Contains(name, "executor"):
		return "executor"
	default:
		return "model"
	}
}

// GetTaskResult retrieves a finished task's record.
func (a *Agent) GetTaskResult(ctx context.Context, tenantID, taskID string) (*domain.OpsAgentResponse, error) {
	task, err := a.taskRepo.Get(ctx, tenantID, taskID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if task == nil {
		return nil, apperr.ErrNotFound
	}

	payload := unmarshalPayload(task.DetailJSON)

	return &domain.OpsAgentResponse{
		TaskID:     task.TaskID,
		Status:     domain.OpsTaskStatus(task.Status),
		Result:     task.Result,
		Detail:     payload.Detail,
		TraceID:    task.TraceID,
		Evidence:   payload.Evidence,
		Conclusion: payload.Conclusion,
		Timing:     buildOpsTiming(task),
	}, nil
}

func (a *Agent) createKnowledgeDraft(ctx context.Context, tenantID, userID, taskID, traceID, query string, evidence []domain.Evidence, conclusion *domain.FaultConclusion) {
	if a.knowledgeRepo == nil || conclusion == nil {
		return
	}
	evidenceRaw, _ := json.Marshal(evidence)
	title := strings.TrimSpace(conclusion.Symptom)
	if title == "" {
		title = truncate(query, 80)
	}
	if title == "" {
		title = "Ops 故障知识草稿"
	}
	_ = a.knowledgeRepo.CreateDraft(ctx, &repository.FaultKnowledge{
		CardID:      "fk_" + uuid.NewString(),
		TenantID:    tenantID,
		TaskID:      taskID,
		TraceID:     traceID,
		Title:       redact.Summary(title, 200),
		Symptom:     redact.Summary(conclusion.Symptom, 1000),
		Impact:      redact.Summary(conclusion.Impact, 1000),
		RootCause:   redact.Summary(conclusion.RootCause, 2000),
		Workaround:  redact.Summary(conclusion.Workaround, 2000),
		Remediation: redact.Summary(conclusion.Remediation, 2000),
		Evidence:    redact.JSON(string(evidenceRaw)),
		Service:     extractService(query + "\n" + conclusion.Symptom + "\n" + conclusion.RootCause),
		Version:     extractVersion(query),
		Status:      "draft",
		Weight:      1.0,
		CreatedBy:   userID,
	})
}

var (
	servicePattern = regexp.MustCompile(`(?i)([a-z][a-z0-9_-]{1,48}-(?:service|svc|api|gateway|worker))`)
	versionPattern = regexp.MustCompile(`(?i)(?:v|version)[-_ ]?([0-9]+(?:\.[0-9]+){1,3})`)
)

func extractService(text string) string {
	if m := servicePattern.FindStringSubmatch(text); len(m) == 2 {
		return strings.ToLower(m[1])
	}
	return ""
}

func extractVersion(text string) string {
	if m := versionPattern.FindStringSubmatch(text); len(m) == 2 {
		return "v" + m[1]
	}
	return ""
}

func buildOpsTiming(task *repository.OpsTask) *domain.OpsTiming {
	if task == nil {
		return nil
	}
	format := func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	timing := &domain.OpsTiming{
		CreatedAt:  task.CreatedAt.UTC().Format(time.RFC3339),
		StartedAt:  format(task.StartedAt),
		FinishedAt: format(task.FinishedAt),
	}
	if task.StartedAt != nil && !task.CreatedAt.IsZero() {
		timing.QueueDurationMS = task.StartedAt.Sub(task.CreatedAt).Milliseconds()
	}
	if task.StartedAt != nil && task.FinishedAt != nil {
		timing.RunDurationMS = task.FinishedAt.Sub(*task.StartedAt).Milliseconds()
	}
	if task.FinishedAt != nil && !task.CreatedAt.IsZero() {
		timing.E2EDurationMS = task.FinishedAt.Sub(task.CreatedAt).Milliseconds()
	}
	return timing
}

// ListTasks returns the page-indexed list of recent ops tasks for a tenant.
func (a *Agent) ListTasks(ctx context.Context, tenantID, statusFilter string, page, size int) ([]domain.OpsTaskSummary, int, error) {
	tasks, total, err := a.taskRepo.ListByTenant(ctx, tenantID, statusFilter, page, size)
	if err != nil {
		return nil, 0, apperr.Wrap(err, apperr.ErrInternal)
	}
	out := make([]domain.OpsTaskSummary, 0, len(tasks))
	for _, t := range tasks {
		createdAt := ""
		if !t.CreatedAt.IsZero() {
			createdAt = t.CreatedAt.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, domain.OpsTaskSummary{
			TaskID:      t.TaskID,
			Status:      t.Status,
			TriggerType: t.TriggerType,
			CreatedAt:   createdAt,
			CreatedBy:   t.CreatedBy,
		})
	}
	return out, total, nil
}

func buildFallbackConclusion(query string, detail []string, evidence []domain.Evidence) string {
	var evidenceLines []string
	for _, e := range evidence {
		evidenceLines = append(evidenceLines, fmt.Sprintf("- %s：%s，耗时 %dms，输入=%s，输出摘要=%s", e.ToolName, e.Status, e.LatencyMS, e.Input, e.Output))
	}
	if len(evidenceLines) == 0 {
		for _, d := range detail {
			if strings.Contains(d, "[error]") {
				evidenceLines = append(evidenceLines, "- "+d)
			}
		}
	}
	if len(evidenceLines) == 0 {
		evidenceLines = append(evidenceLines, "- 暂无工具证据，需检查模型工具调用兼容性或外部系统配置。")
	}

	confidence := "low"
	rootCause := "当前没有足够的成功工具证据，无法确认根因。"
	impact := "当前无法基于实时证据确认影响范围。"
	for _, e := range evidence {
		if e.Status == "success" {
			confidence = "mid"
			rootCause = "已获取部分实时工具证据，仍需结合其他数据源交叉验证。"
			impact = "影响范围需要结合已返回的工具证据进一步确认。"
			break
		}
	}
	return fmt.Sprintf(`故障现象：
%s

影响范围：
%s

根因判断：
%s

临时止血：
仅执行经过人工确认且与成功工具证据一致的止血操作。

根治建议：
补齐失败数据源并继续进行日志、指标、发布记录交叉验证。

证据链：
%s

置信度：%s`, strings.TrimSpace(query), impact, rootCause, strings.Join(evidenceLines, "\n"), confidence)
}

const defaultOpsQuery = `你是一个智能运维告警排障助手。请按以下步骤排查最近的服务告警，并在最后输出结构化结论。

排查步骤：
1. 调用 get_current_time 获取当前时间作为排障基准。
2. 调用 query_prometheus_alerts 获取当前正在触发的告警。
3. 若告警涉及具体服务，调用 query_metric_range 查询该服务关键指标区间数据（错误率、延迟、CPU、内存等），观察故障前后趋势。
4. 若告警携带 TraceID，调用 query_logs_by_trace 按链路检索日志，定位慢调用或错误节点；否则用 query_logs 按关键词查询日志。
5. 调用 query_deployments 查询该服务近期发布、灰度、回滚记录，判断是否由变更引发。
6. 若告警涉及具体服务，使用 query_internal_docs 检索该服务的处置手册。
7. 综合以上信息输出标准化结论，必须包含：故障现象、影响范围、根因判断、临时止血、根治建议、置信度。

输出要求：在回答结尾，必须使用以下中文小节输出结构化结论，每节标题独占一行：
故障现象：
影响范围：
根因判断：
临时止血：
根治建议：
置信度：high/mid/low`

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
