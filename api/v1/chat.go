package v1

import "github.com/gogf/gf/v2/frame/g"

// ChatReq is a synchronous chat request.
type ChatReq struct {
	g.Meta    `path:"/chat" method:"post" tags:"Chat" summary:"同步对话"`
	SessionID string `json:"session_id" v:"required"`
	Question  string `json:"question" v:"required"`
	Options   *ChatOptions `json:"options"`
}

// ChatOptions configures chat behavior.
type ChatOptions struct {
	EnableRAG   bool `json:"enable_rag"`
	EnableTools bool `json:"enable_tools"`
}

// ChatRes is a synchronous chat response.
type ChatRes struct {
	SessionID string `json:"session_id"`
	Answer    string `json:"answer"`
	TraceID   string `json:"trace_id"`
}

// ChatStreamReq is an SSE streaming chat request.
type ChatStreamReq struct {
	g.Meta    `path:"/chat/stream" method:"post" tags:"Chat" summary:"流式对话"`
	SessionID string `json:"session_id" v:"required"`
	Question  string `json:"question" v:"required"`
	Options   *ChatOptions `json:"options"`
}

// ChatStreamRes is intentionally empty; SSE writes directly to response.
type ChatStreamRes struct{}
