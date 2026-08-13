package domain

import (
	"context"
	"encoding/json"
)

// Message is a chat message stored in Redis session history.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}

// SessionConfig controls session creation.
type SessionConfig struct {
	Title     string
	AgentType string
}

// SessionOption is a functional option for session creation.
type SessionOption func(*SessionConfig)

// DefaultSessionConfig returns the default session config.
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{}
}

// WithSessionTitle sets the session title.
func WithSessionTitle(title string) SessionOption {
	return func(c *SessionConfig) { c.Title = title }
}

// WithSessionAgentType sets the agent type.
func WithSessionAgentType(agentType string) SessionOption {
	return func(c *SessionConfig) { c.AgentType = agentType }
}

// SessionSummary is a session list entry.
type SessionSummary struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	AgentType string `json:"agent_type"`
	UpdatedAt string `json:"updated_at"`
}

// SessionService manages conversation history.
type SessionService interface {
	GetHistory(ctx context.Context, tenantID, sessionID string) ([]*Message, error)
	AppendMessages(ctx context.Context, tenantID, sessionID string, msgs ...*Message) error
	CreateSession(ctx context.Context, tenantID, userID string, opts ...SessionOption) (sessionID string, err error)
	UpdateSessionTitle(ctx context.Context, tenantID, sessionID, title string) error
	DeleteSession(ctx context.Context, tenantID, sessionID string) error
	ListSessions(ctx context.Context, tenantID, userID string, page, size int) ([]SessionSummary, int, error)
	GetSession(ctx context.Context, tenantID, sessionID string) (*SessionSummary, error)
}

// RetrieveRequest carries RAG retrieval parameters.
type RetrieveRequest struct {
	TenantID       string
	Query          string
	TopK           int
	DocIDs         []string
	MinScore       float64
	MaxSecretLevel int // 0 = derive from caller roles; otherwise filter metadata secret_level
	ExcludeSources []string
}

// KnowledgeLayer identifies which vector collection / knowledge tier a hit came from.
type KnowledgeLayer string

const (
	// KnowledgeLayerStatic = 稳定静态库（高置信度、低更新）
	KnowledgeLayerStatic KnowledgeLayer = "static"
	// KnowledgeLayerFaultCase = 故障案例库（中置信度、增量更新）
	KnowledgeLayerFaultCase KnowledgeLayer = "fault_case"
	// KnowledgeLayerTemp = 临时知识区（低置信度、高频更新）
	KnowledgeLayerTemp KnowledgeLayer = "temp"
)

// ConfidenceLevel is a coarse-grained trust label for a RAG result set.
type ConfidenceLevel string

const (
	ConfidenceHigh ConfidenceLevel = "high" // >= 0.75
	ConfidenceMid  ConfidenceLevel = "mid"  // 0.5 ~ 0.75
	ConfidenceLow  ConfidenceLevel = "low"  // < 0.5
)

// RetrievedDocument is a single RAG hit.
type RetrievedDocument struct {
	ChunkID  string
	DocID    string
	Content  string
	Source   string
	Score    float64 // 归一化后的相似度分数 [0,1]，越大越相关
	RawScore float64 // 原始向量距离（L2 等），仅用于诊断
	Layer    KnowledgeLayer
	Version  string
	Service  string
	Metadata map[string]any
}

// RetrieveResponse wraps RAG results.
type RetrieveResponse struct {
	Documents  []RetrievedDocument
	Confidence ConfidenceLevel // 综合置信度，由最高分 + 层级权重决定
	TopScore   float64         // 归一化后最高分，供编排层做路径决策
}

// DocumentIndexGeneration is the read-side publication state for one document.
// Generation 0 is only readable when LegacyAllowed is true.
type DocumentIndexGeneration struct {
	ActiveGeneration uint64
	LegacyAllowed    bool
}

// IndexTaskRequest submits a document for indexing.
type IndexTaskRequest struct {
	TaskID      string
	Generation  uint64
	TenantID    string
	DocID       string
	SourceURI   string
	Visibility  string
	SecretLevel int
	Layer       KnowledgeLayer
	Version     string
	Service     string
}

// IndexTaskExecutor executes an existing knowledge index task.
type IndexTaskExecutor interface {
	ExecuteIndexTask(ctx context.Context, tenantID, taskID, executionToken string) error
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
	Route(ctx context.Context, req *RetrieveRequest) (*RAGRouteDecision, error)
	SubmitIndexTask(ctx context.Context, req *IndexTaskRequest) (taskID string, err error)
	GetIndexTask(ctx context.Context, tenantID, taskID string) (*IndexTask, error)
	DeleteDocumentChunks(ctx context.Context, tenantID, docID string) error
}

// RAGRouteDecision is a lightweight orchestration decision based on retrieval confidence.
type RAGRouteDecision struct {
	Response   *RetrieveResponse
	Confidence ConfidenceLevel
	Route      string // fast_answer | agent_rag | realtime_tools
	Reason     string
}

// ToolMeta describes a registered tool.
type ToolMeta struct {
	Name        string
	Description string
	RiskLevel   ToolRiskLevel
	TimeoutMS   int
	Agents      []AgentType
	Enabled     bool
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

// ModelRouter resolves LLM clients by profile.
type ModelRouter interface {
	ChatModel(ctx context.Context, profile ModelProfile) (any, error)
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
	// Version is the published document version/generation when available.
	// It is optional for legacy documents and keeps old clients compatible.
	Version string `json:"version,omitempty"`
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
	TriggerType   string
}

// Evidence captures one tool call performed by the Ops Agent, so the Portal
// can render a structured proof chain instead of free-text narration.
type Evidence struct {
	ToolName  string `json:"tool_name"`
	Input     string `json:"input,omitempty"`  // 入参摘要
	Output    string `json:"output,omitempty"` // 出参摘要
	Status    string `json:"status"`           // success / error / awaiting_approval
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// OpsTiming captures queue, run and end-to-end troubleshooting latency.
type OpsTiming struct {
	QueueDurationMS int64  `json:"queue_duration_ms"`
	RunDurationMS   int64  `json:"run_duration_ms"`
	E2EDurationMS   int64  `json:"e2e_duration_ms"`
	CreatedAt       string `json:"created_at,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	FinishedAt      string `json:"finished_at,omitempty"`
}

// FaultConclusion is the structured output of an Ops troubleshooting run,
// mirroring §6.2 step 5 of the refactor design doc.
type FaultConclusion struct {
	Symptom     string `json:"symptom"`     // 故障现象
	Impact      string `json:"impact"`      // 影响范围
	RootCause   string `json:"root_cause"`  // 根因判断
	Workaround  string `json:"workaround"`  // 临时止血方案
	Remediation string `json:"remediation"` // 根治建议
	Confidence  string `json:"confidence"`  // high / mid / low
	Source      string `json:"source"`      // 实时排查结论 / 历史方案
}

// OpsAgentResponse is the ops agent output.
type OpsAgentResponse struct {
	TaskID     string
	Status     OpsTaskStatus
	Result     string
	Detail     []string
	TraceID    string
	Evidence   []Evidence
	Conclusion *FaultConclusion
	Timing     *OpsTiming
}

// StreamReader reads SSE chunks from a streaming chat response.
type StreamReader interface {
	Next() (event string, data string, ok bool)
	Close() error
}

// ChatAgent executes chat conversations.
type ChatAgent interface {
	Invoke(ctx context.Context, req *ChatAgentRequest) (*ChatAgentResponse, error)
	Stream(ctx context.Context, req *ChatAgentRequest) (StreamReader, error)
}

// OpsAgent performs Ops alert analysis.
type OpsAgent interface {
	Analyze(ctx context.Context, req *OpsAgentRequest) (*OpsAgentResponse, error)
	ExecuteTask(ctx context.Context, tenantID, taskID, executionToken string) (*OpsAgentResponse, error)
	GetTaskResult(ctx context.Context, tenantID, taskID string) (*OpsAgentResponse, error)
	ListTasks(ctx context.Context, tenantID, statusFilter string, page, size int) ([]OpsTaskSummary, int, error)
}

// OpsTaskSummary is a lightweight projection of an ops task for list views.
type OpsTaskSummary struct {
	TaskID      string
	Status      string
	TriggerType string
	CreatedAt   string
	CreatedBy   string
}

// RouteRequest is input to the intent router.
type RouteRequest struct {
	Path      string
	Query     string
	AgentType AgentType
	TenantID  string // optional; if empty the router reads it from ctx
}

// IntentRouter selects the target agent type.
type IntentRouter interface {
	Route(ctx context.Context, req *RouteRequest) (AgentType, error)
}
