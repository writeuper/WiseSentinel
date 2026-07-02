// Package chat implements the Chat Agent using Eino's ReAct framework.
package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/trace"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	systemPromptTpl = `你是智哨(WiseSentinel)智能运维助手，负责处理运维相关的问题。

回答规则：
- 回答必须基于提供的文档与工具返回结果，不得编造信息
- 引用文档时标注来源
- 保持专业、简洁的运维风格

当前时间：%s
相关文档：
%s`
	maxStep = 25
)

// UserMessage is the internal chat agent request.
type UserMessage struct {
	TenantID  string
	SessionID string
	UserID    string
	Query     string
	History   []*domain.Message
	Options   domain.ChatOptions
}

// ChatResult is the internal chat agent response.
type ChatResult struct {
	Answer    string
	Citations []domain.Citation
	ToolCalls []domain.ToolCallSummary
	TraceID   string
}

// Agent implements the Chat Agent using Eino's ReAct agent.
type Agent struct {
	modelRouter domain.ModelRouter
	ragService  domain.RAGService
	toolGateway domain.ToolGateway
}

// NewAgent creates a chat agent.
func NewAgent(modelRouter domain.ModelRouter, ragService domain.RAGService, toolGateway domain.ToolGateway) *Agent {
	return &Agent{
		modelRouter: modelRouter,
		ragService:  ragService,
		toolGateway: toolGateway,
	}
}

// Invoke processes a synchronous chat request.
func (a *Agent) Invoke(ctx context.Context, req *UserMessage) (*ChatResult, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	// 1. Retrieve RAG docs if enabled
	documents, citations := a.retrieveDocs(ctx, req)

	// 2. Build the ReAct agent
	reactAgent, err := a.buildReActAgent(ctx, req, documents, traceID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	// 3. Build input messages: history + user query
	input := a.buildInputMessages(req)

	// 4. Generate response
	result, err := reactAgent.Generate(ctx, input)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrAgentFailed)
	}

	return &ChatResult{
		Answer:    result.Content,
		Citations: citations,
		ToolCalls: extractToolCallSummary(result),
		TraceID:   traceID,
	}, nil
}

// Stream processes a streaming chat request.
func (a *Agent) Stream(ctx context.Context, req *UserMessage) (<-chan StreamEvent, error) {
	events := make(chan StreamEvent, 64)
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	go func() {
		defer close(events)

		events <- StreamEvent{Type: "connected", Data: fmt.Sprintf(`{"status":"connected","session_id":"%s"}`, req.SessionID)}

		// 1. Retrieve RAG docs
		documents, _ := a.retrieveDocs(ctx, req)

		// 2. Build the ReAct agent
		reactAgent, err := a.buildReActAgent(ctx, req, documents, traceID)
		if err != nil {
			events <- StreamEvent{Type: "error", Data: fmt.Sprintf("Agent 构建失败: %v", err)}
			events <- StreamEvent{Type: "done", Data: fmt.Sprintf(`{"trace_id":"%s"}`, traceID)}
			return
		}

		// 3. Build input messages
		input := a.buildInputMessages(req)

		// 4. Stream response
		sr, err := reactAgent.Stream(ctx, input)
		if err != nil {
			errMsg := fmt.Sprintf("Stream 失败: %v", err)
			// Try to unwrap for more details
			if unwrapped := fmt.Sprintf("%+v", err); unwrapped != err.Error() {
				errMsg = fmt.Sprintf("Stream 失败: %v (detail: %s)", err, unwrapped)
			}
			g.Log().Errorf(ctx, "ChatAgent.Stream reactAgent.Stream error: %+v", err)
			events <- StreamEvent{Type: "error", Data: errMsg}
			events <- StreamEvent{Type: "done", Data: fmt.Sprintf(`{"trace_id":"%s"}`, traceID)}
			return
		}
		defer sr.Close()

		for {
			msg, err := sr.Recv()
			if err != nil {
				break
			}
			if msg.Content != "" {
				events <- StreamEvent{Type: "message", Data: msg.Content}
			}
		}
		events <- StreamEvent{Type: "done", Data: fmt.Sprintf(`{"trace_id":"%s"}`, traceID)}
	}()

	return events, nil
}

// StreamEvent represents an SSE event for streaming.
type StreamEvent struct {
	Type string
	Data string
}

// buildReActAgent creates a new ReAct agent with the appropriate configuration.
func (a *Agent) buildReActAgent(ctx context.Context, req *UserMessage, documents string, traceID string) (*react.Agent, error) {
	// 1. Get the chat model
	rawModel, err := a.modelRouter.ChatModel(ctx, domain.ModelProfileChatFast)
	if err != nil {
		return nil, fmt.Errorf("model router: %w", err)
	}
	chatModel, ok := rawModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("model does not implement ToolCallingChatModel")
	}

	// 2. Get tools for the chat agent
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = ctxkeys.TenantIDFrom(ctx)
	}
	einoTools := make([]tool.BaseTool, 0)
	if req.Options.EnableTools && a.toolGateway != nil {
		// Type-assert to access Eino-specific AsEinoTools method
		type einoToolLister interface {
			AsEinoTools(ctx context.Context, tenantID string, agentType domain.AgentType) ([]tool.BaseTool, error)
		}
		if lister, ok := a.toolGateway.(einoToolLister); ok {
			einoTools, err = lister.AsEinoTools(ctx, tenantID, domain.AgentTypeChat)
			if err != nil {
				return nil, fmt.Errorf("list tools: %w", err)
			}
		}
	}

	// 3. Build system prompt
	now := time.Now().Format("2006-01-02 15:04:05 MST")
	systemPrompt := fmt.Sprintf(systemPromptTpl, now, documents)

	// 4. Create MessageModifier to inject system prompt on first call only
	var once sync.Once
	modifier := func(_ context.Context, input []*schema.Message) []*schema.Message {
		var result []*schema.Message
		once.Do(func() {
			result = make([]*schema.Message, 0, len(input)+1)
			result = append(result, schema.SystemMessage(systemPrompt))
			result = append(result, input...)
		})
		if result != nil {
			return result
		}
		return input
	}

	// 5. Create ReAct agent config
	config := &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: einoTools,
		},
		MessageModifier: modifier,
		MaxStep:         maxStep,
		GraphName:       "ChatAgent",
	}

	agent, err := react.NewAgent(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("new react agent: %w", err)
	}

	return agent, nil
}

// buildInputMessages builds the []*schema.Message from the user request.
func (a *Agent) buildInputMessages(req *UserMessage) []*schema.Message {
	messages := make([]*schema.Message, 0, len(req.History)+1)

	// Add history messages
	for _, h := range req.History {
		role := schema.RoleType(h.Role)
		messages = append(messages, &schema.Message{
			Role:    role,
			Content: h.Content,
		})
	}

	// Add current user query
	messages = append(messages, schema.UserMessage(req.Query))

	return messages
}

// retrieveDocs retrieves RAG documents if enabled.
func (a *Agent) retrieveDocs(ctx context.Context, req *UserMessage) (string, []domain.Citation) {
	var documents string
	var citations []domain.Citation

	if !req.Options.EnableRAG || a.ragService == nil {
		return documents, citations
	}

	ragResp, err := a.ragService.Retrieve(ctx, &domain.RetrieveRequest{
		TenantID: req.TenantID,
		Query:    req.Query,
		TopK:     3,
	})
	if err != nil || ragResp == nil {
		return documents, citations
	}

	for _, doc := range ragResp.Documents {
		documents += fmt.Sprintf("来源[%s]: %s\n---\n", doc.Source, doc.Content)
		citations = append(citations, domain.Citation{
			DocID:   doc.DocID,
			ChunkID: doc.ChunkID,
			Source:  doc.Source,
			Snippet: truncate(doc.Content, 200),
		})
	}

	return documents, citations
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