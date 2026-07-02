package bootstrap

import (
	"context"
	"encoding/json"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/trace"
	"wisesentinel-platform/internal/repository"

	"github.com/google/uuid"
)

// OpsAgentPlaceholder is a temporary M3 placeholder for the Ops Agent.
// M4 will replace this with the full Plan-Execute-Replan implementation.
type OpsAgentPlaceholder struct {
	modelRouter domain.ModelRouter
	ragService  domain.RAGService
	toolGateway domain.ToolGateway
	taskRepo    *repository.OpsTaskRepo
}

// NewOpsAgentPlaceholder creates a placeholder ops agent.
func NewOpsAgentPlaceholder(
	modelRouter domain.ModelRouter,
	ragService domain.RAGService,
	toolGateway domain.ToolGateway,
	taskRepo *repository.OpsTaskRepo,
) *OpsAgentPlaceholder {
	return &OpsAgentPlaceholder{
		modelRouter: modelRouter,
		ragService:  ragService,
		toolGateway: toolGateway,
		taskRepo:    taskRepo,
	}
}

// ChatInvoke is not supported for Ops agent.
func (a *OpsAgentPlaceholder) ChatInvoke(ctx context.Context, req *domain.ChatAgentRequest) (*domain.ChatAgentResponse, error) {
	return nil, apperr.New(50002, 500, "Ops agent does not support chat")
}

// ChatStream is not supported for Ops agent.
func (a *OpsAgentPlaceholder) ChatStream(ctx context.Context, req *domain.ChatAgentRequest) (domain.StreamReader, error) {
	return nil, apperr.New(50002, 500, "Ops agent does not support chat stream")
}

// OpsAnalyze performs a simple analysis by creating a task and returning a placeholder result.
func (a *OpsAgentPlaceholder) OpsAnalyze(ctx context.Context, req *domain.OpsAgentRequest) (*domain.OpsAgentResponse, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	taskID := "ops_" + uuid.NewString()
	userID := ctxkeys.UserIDFrom(ctx)

	// Create task record
	task := &repository.OpsTask{
		TenantID:    req.TenantID,
		TaskID:      taskID,
		TriggerType: "manual",
		InputQuery:  req.Query,
		Status:      string(domain.OpsTaskRunning),
		TraceID:     traceID,
		CreatedBy:   userID,
	}
	if err := a.taskRepo.Create(ctx, task); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	if req.Async {
		// Run in background
		go a.runAsync(context.Background(), taskID, req)
		return &domain.OpsAgentResponse{
			TaskID:  taskID,
			Status:  domain.OpsTaskPending,
			TraceID: traceID,
		}, nil
	}

	// Synchronous: produce a simple analysis
	result := a.generateAnalysis(ctx, req)
	detailJSON, _ := json.Marshal([]string{"分析完成"})
	_ = a.taskRepo.MarkFinished(ctx, req.TenantID, taskID,
		string(domain.OpsTaskSuccess), result, string(detailJSON))

	return &domain.OpsAgentResponse{
		TaskID:  taskID,
		Status:  domain.OpsTaskSuccess,
		Result:  result,
		Detail:  []string{"分析完成"},
		TraceID: traceID,
	}, nil
}

func (a *OpsAgentPlaceholder) runAsync(ctx context.Context, taskID string, req *domain.OpsAgentRequest) {
	_ = a.taskRepo.MarkRunning(ctx, req.TenantID, taskID)
	result := a.generateAnalysis(ctx, req)
	detailJSON, _ := json.Marshal([]string{"异步分析完成"})
	_ = a.taskRepo.MarkFinished(ctx, req.TenantID, taskID,
		string(domain.OpsTaskSuccess), result, string(detailJSON))
}

func (a *OpsAgentPlaceholder) generateAnalysis(ctx context.Context, req *domain.OpsAgentRequest) string {
	query := req.Query
	if query == "" {
		query = "告警分析"
	}
	return `# 告警分析报告

## 分析结果
- 时间: ` + time.Now().Format("2006-01-02 15:04:05") + `
- 分析查询: ` + query + `

## 建议
> 此分析由 M3 初步版本生成，完整 Ops Agent 将在 M4 实现。
> 如需完整分析，请启用 Prometheus 并配置 LLM。`
}

// GetTaskResult retrieves an ops task result.
func (a *OpsAgentPlaceholder) GetTaskResult(ctx context.Context, tenantID, taskID string) (*domain.OpsAgentResponse, error) {
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