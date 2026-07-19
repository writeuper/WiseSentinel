// Package ops implements the Ops Agent using Eino v0.6.0's Plan-Execute-Replan pattern.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
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
	modelRouter domain.ModelRouter
	toolGateway domain.ToolGateway
	taskRepo    *repository.OpsTaskRepo
	maxIter     int
}

// NewAgent creates a new Ops Agent.
func NewAgent(modelRouter domain.ModelRouter, toolGateway domain.ToolGateway, taskRepo *repository.OpsTaskRepo) *Agent {
	return &Agent{
		modelRouter: modelRouter,
		toolGateway: toolGateway,
		taskRepo:    taskRepo,
		maxIter:     20, // matches design doc §6.2
	}
}

// Analyze runs the Ops agent for the given request.
//
//   - sync mode: runs the agent and returns the final result inline.
//   - async mode: creates a row in ws_ops_task, kicks off a background run,
//     and returns immediately with status=pending.
func (a *Agent) Analyze(ctx context.Context, req *domain.OpsAgentRequest) (*domain.OpsAgentResponse, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	taskID := "ops_" + uuid.NewString()
	userID := ctxkeys.UserIDFrom(ctx)
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}

	// 1. Create the task row (pending).
	task := &repository.OpsTask{
		TenantID:    tenantID,
		TaskID:      taskID,
		TriggerType: "manual",
		InputQuery:  req.Query,
		Status:      string(domain.OpsTaskPending),
		TraceID:     traceID,
		CreatedBy:   userID,
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

	// Sync requests execute the task inline.
	return a.executeTask(ctx, tenantID, taskID, traceID, req)
}

// ExecuteTask executes an existing persisted task. Workers must use this
// method instead of Analyze so task execution never creates another task.
func (a *Agent) ExecuteTask(ctx context.Context, tenantID, taskID string) (*domain.OpsAgentResponse, error) {
	task, err := a.taskRepo.Get(ctx, tenantID, taskID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if task == nil {
		return nil, apperr.ErrNotFound
	}
	traceID := task.TraceID
	if traceID == "" {
		traceID = trace.NewID()
	}
	ctx = ctxkeys.WithTraceID(ctx, traceID)
	return a.executeTask(ctx, tenantID, taskID, traceID, &domain.OpsAgentRequest{
		TenantID: tenantID,
		UserID:   task.CreatedBy,
		Query:    task.InputQuery,
		Async:    false,
	})
}

func (a *Agent) executeTask(ctx context.Context, tenantID, taskID, traceID string, req *domain.OpsAgentRequest) (*domain.OpsAgentResponse, error) {
	_ = a.taskRepo.MarkRunning(ctx, tenantID, taskID)
	result, detail, err := a.runAgent(ctx, tenantID, req)
	if err != nil {
		detailJSON, _ := json.Marshal(detail)
		status := domain.OpsTaskFailed
		if errors.Is(err, context.DeadlineExceeded) {
			status = domain.OpsTaskTimeout
		}
		_ = a.taskRepo.MarkFinished(ctx, tenantID, taskID,
			string(status), err.Error(), string(detailJSON))
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	detailJSON, _ := json.Marshal(detail)
	_ = a.taskRepo.MarkFinished(ctx, tenantID, taskID,
		string(domain.OpsTaskSuccess), result, string(detailJSON))
	return &domain.OpsAgentResponse{
		TaskID:  taskID,
		Status:  domain.OpsTaskSuccess,
		Result:  result,
		Detail:  detail,
		TraceID: traceID,
	}, nil
}

// runAgent builds and runs the Plan-Execute-Replan graph.
func (a *Agent) runAgent(ctx context.Context, tenantID string, req *domain.OpsAgentRequest) (string, []string, error) {
	// 1. Build planner / executor / replanner.
	plannerAgent, err := NewPlanner(ctx, a.modelRouter)
	if err != nil {
		return "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}
	executorAgent, err := NewExecutor(ctx, a.modelRouter, a.toolGateway, tenantID)
	if err != nil {
		return "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}
	replannerAgent, err := NewReplanner(ctx, a.modelRouter)
	if err != nil {
		return "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	// 2. Wire the plan-execute-replan agent.
	planExecuteAgent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       plannerAgent,
		Executor:      executorAgent,
		Replanner:     replannerAgent,
		MaxIterations: a.maxIter,
	})
	if err != nil {
		return "", nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	// 3. Run via the ADK Runner.
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: planExecuteAgent})

	query := req.Query
	if strings.TrimSpace(query) == "" {
		query = defaultOpsQuery
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
			g.Log().Errorf(ctx, "ops agent event error: %v", event.Err)
			detail = append(detail, fmt.Sprintf("[error] %v", event.Err))
			continue
		}
		if event.Output != nil {
			msg, _, mErr := adk.GetMessage(event)
			if mErr == nil && msg != nil {
				lastMessage = msg
				detail = append(detail, fmt.Sprintf("[%s] %s", event.AgentName, truncate(msg.Content, 400)))
			}
		}
	}

	if lastMessage == nil {
		return "", detail, apperr.New(50002, 500, "ops agent produced no output")
	}
	result = lastMessage.Content
	return result, detail, nil
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

	var detail []string
	if task.DetailJSON != "" {
		_ = json.Unmarshal([]byte(task.DetailJSON), &detail)
	}

	return &domain.OpsAgentResponse{
		TaskID:  task.TaskID,
		Status:  domain.OpsTaskStatus(task.Status),
		Result:  task.Result,
		Detail:  detail,
		TraceID: task.TraceID,
	}, nil
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

const defaultOpsQuery = `你是一个智能运维告警分析助手。请按以下步骤分析最近的服务告警：

1. 调用 get_current_time 获取当前时间作为分析基准。
2. 调用 query_prometheus_alerts 获取当前正在触发的告警。
3. 若告警涉及具体服务，使用 query_internal_docs 检索该服务的处置手册。
4. 若需要进一步日志证据，使用 query_logs 查询相关日志。
5. 综合以上信息给出根因分析与处置建议。`

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
