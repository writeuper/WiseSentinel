package domain

import (
	"context"
	"encoding/json"
)

// Message is a chat message placeholder until Eino schema.Message is wired in M3.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SessionService manages conversation history.
type SessionService interface {
	GetHistory(ctx context.Context, tenantID, sessionID string) ([]*Message, error)
	AppendMessages(ctx context.Context, tenantID, sessionID string, msgs ...*Message) error
	CreateSession(ctx context.Context, tenantID, userID string) (sessionID string, err error)
}

// RetrieveRequest carries RAG retrieval parameters.
type RetrieveRequest struct {
	TenantID string
	Query    string
	TopK     int
	DocIDs   []string
	MinScore float64
}

// RetrievedDocument is a single RAG hit.
type RetrievedDocument struct {
	ChunkID  string
	DocID    string
	Content  string
	Source   string
	Score    float64
	Metadata map[string]any
}

// RetrieveResponse wraps RAG results.
type RetrieveResponse struct {
	Documents []RetrievedDocument
}

// IndexTaskRequest submits a document for indexing.
type IndexTaskRequest struct {
	TenantID    string
	DocID       string
	SourceURI   string
	Visibility  string
	SecretLevel int
}

// IndexTask represents an async index job.
type IndexTask struct {
	TaskID     string
	DocID      string
	Status     IndexTaskStatus
	ChunkCount int
	ErrorMsg   string
}

// RAGService provides retrieval and indexing.
type RAGService interface {
	Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResponse, error)
	SubmitIndexTask(ctx context.Context, req *IndexTaskRequest) (taskID string, err error)
	GetIndexTask(ctx context.Context, tenantID, taskID string) (*IndexTask, error)
	DeleteDocumentChunks(ctx context.Context, docID string) error
}

// ToolMeta describes a registered tool.
type ToolMeta struct {
	Name      string
	RiskLevel ToolRiskLevel
	TimeoutMS int
	Agents    []AgentType
	Enabled   bool
}

// ToolInvokeRequest is a tool call from an agent.
type ToolInvokeRequest struct {
	TenantID  string
	UserID    string
	TraceID   string
	ToolName  string
	Input     json.RawMessage
	AgentType AgentType
}

// ToolInvokeResponse is the result of a tool call.
type ToolInvokeResponse struct {
	Output     string
	Status     string
	LatencyMS  int
	ApprovalID string
}

// ToolGateway routes tool invocations to adapters.
type ToolGateway interface {
	ListTools(ctx context.Context, tenantID string, agentType AgentType) ([]ToolMeta, error)
	Invoke(ctx context.Context, req *ToolInvokeRequest) (*ToolInvokeResponse, error)
}

// ModelProfile selects an LLM or embedding configuration.
type ModelProfile string

const (
	ModelProfileChatFast         ModelProfile = "chat_fast"
	ModelProfileOpsPlan          ModelProfile = "ops_plan"
	ModelProfileOpsExec          ModelProfile = "ops_exec"
	ModelProfileEmbeddingDefault ModelProfile = "embedding_default"
)

// ModelRouter resolves LLM and embedding clients by profile (implemented in M3).
type ModelRouter interface {
	ChatModel(ctx context.Context, profile ModelProfile) (any, error)
	Embedder(ctx context.Context, profile ModelProfile) (any, error)
}

// ChatOptions configures chat agent behavior.
type ChatOptions struct {
	EnableRAG   bool
	EnableTools bool
}

// ChatAgentRequest is input to the chat agent.
type ChatAgentRequest struct {
	TenantID  string
	SessionID string
	UserID    string
	Query     string
	History   []*Message
	Options   ChatOptions
}

// Citation references a RAG document chunk.
type Citation struct {
	DocID   string `json:"doc_id"`
	ChunkID string `json:"chunk_id"`
	Source  string `json:"source"`
	Snippet string `json:"snippet"`
}

// ToolCallSummary summarizes a tool invocation in chat response.
type ToolCallSummary struct {
	Tool      string `json:"tool"`
	Status    string `json:"status"`
	LatencyMS int    `json:"latency_ms"`
}

// ChatAgentResponse is the chat agent output.
type ChatAgentResponse struct {
	SessionID string
	Answer    string
	Citations []Citation
	ToolCalls []ToolCallSummary
	TraceID   string
}

// OpsAgentRequest triggers ops analysis.
type OpsAgentRequest struct {
	TenantID      string
	UserID        string
	Query         string
	MaxIterations int
	Async         bool
}

// OpsAgentResponse is the ops agent output.
type OpsAgentResponse struct {
	TaskID  string
	Status  OpsTaskStatus
	Result  string
	Detail  []string
	TraceID string
}

// StreamReader reads SSE chunks from a streaming chat response.
type StreamReader interface {
	Next() (event string, data string, ok bool)
	Close() error
}

// AgentRunner executes chat and ops agents.
type AgentRunner interface {
	ChatInvoke(ctx context.Context, req *ChatAgentRequest) (*ChatAgentResponse, error)
	ChatStream(ctx context.Context, req *ChatAgentRequest) (StreamReader, error)
	OpsAnalyze(ctx context.Context, req *OpsAgentRequest) (*OpsAgentResponse, error)
}

// RouteRequest is input to the intent router.
type RouteRequest struct {
	Path      string
	Query     string
	AgentType AgentType
}

// IntentRouter selects the target agent type.
type IntentRouter interface {
	Route(ctx context.Context, req *RouteRequest) (AgentType, error)
}
