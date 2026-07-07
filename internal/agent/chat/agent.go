// Package chat implements the Chat Agent using Eino's ReAct framework.
package chat

import (
	"context"
	"fmt"

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

const maxStep = 25

// Agent implements domain.ChatAgent using Eino's ReAct agent.
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
func (a *Agent) Invoke(ctx context.Context, req *domain.ChatAgentRequest) (*domain.ChatAgentResponse, error) {
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

	return &domain.ChatAgentResponse{
		SessionID: req.SessionID,
		Answer:    result.Content,
		Citations: citations,
		ToolCalls: extractToolCallSummary(result),
		TraceID:   traceID,
	}, nil
}

// Stream processes a streaming chat request and returns a StreamReader.
func (a *Agent) Stream(ctx context.Context, req *domain.ChatAgentRequest) (domain.StreamReader, error) {
	traceID := ctxkeys.TraceIDFrom(ctx)
	if traceID == "" {
		traceID = trace.NewID()
	}

	r := &chatStreamReader{
		events: make(chan streamEventItem, 64),
	}

	go func() {
		defer close(r.events)
		r.send("connected", fmt.Sprintf(`{"status":"connected","session_id":"%s"}`, req.SessionID))

		// 1. Retrieve RAG docs
		documents, _ := a.retrieveDocs(ctx, req)

		// 2. Build the ReAct agent
		reactAgent, err := a.buildReActAgent(ctx, req, documents, traceID)
		if err != nil {
			r.send("error", fmt.Sprintf("Agent 构建失败: %v", err))
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}

		// 3. Build input messages
		input := a.buildInputMessages(req)

		// 4. Stream response
		sr, err := reactAgent.Stream(ctx, input)
		if err != nil {
			errMsg := fmt.Sprintf("Stream 失败: %v", err)
			if unwrapped := fmt.Sprintf("%+v", err); unwrapped != err.Error() {
				errMsg = fmt.Sprintf("Stream 失败: %v (detail: %s)", err, unwrapped)
			}
			g.Log().Errorf(ctx, "ChatAgent.Stream reactAgent.Stream error: %+v", err)
			r.send("error", errMsg)
			r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
			return
		}
		defer sr.Close()

		for {
			msg, err := sr.Recv()
			if err != nil {
				break
			}
			if msg.Content != "" {
				r.send("message", msg.Content)
			}
		}
		r.send("done", fmt.Sprintf(`{"trace_id":"%s"}`, traceID))
	}()

	return r, nil
}

// streamEventItem holds a single SSE-like event.
type streamEventItem struct {
	event string
	data  string
}

// chatStreamReader implements domain.StreamReader over a channel.
type chatStreamReader struct {
	events chan streamEventItem
}

func (r *chatStreamReader) send(event, data string) {
	r.events <- streamEventItem{event: event, data: data}
}

// Next returns the next event (event type, data payload, and whether more events exist).
func (r *chatStreamReader) Next() (event string, data string, ok bool) {
	item, more := <-r.events
	if !more {
		return "", "", false
	}
	return item.event, item.data, true
}

// Close terminates the stream.
func (r *chatStreamReader) Close() error {
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
func (a *Agent) buildInputMessages(req *domain.ChatAgentRequest) []*schema.Message {
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
func (a *Agent) retrieveDocs(ctx context.Context, req *domain.ChatAgentRequest) (string, []domain.Citation) {
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
