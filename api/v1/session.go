package v1

import "github.com/gogf/gf/v2/frame/g"

// CreateSessionReq creates a new conversation session.
type CreateSessionReq struct {
	g.Meta    `path:"/sessions" method:"post" tags:"Session" summary:"创建会话"`
	Title     string `json:"title"`
	AgentType string `json:"agent_type" d:"chat"`
}

// CreateSessionRes returns the new session identifier.
type CreateSessionRes struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// ListSessionsReq lists user sessions.
type ListSessionsReq struct {
	g.Meta `path:"/sessions" method:"get" tags:"Session" summary:"会话列表"`
	Page   int `json:"page" d:"1" in:"query"`
	Size   int `json:"size" d:"20" in:"query"`
}

// ListSessionsRes returns paginated sessions.
type ListSessionsRes struct {
	Items []SessionItem `json:"items"`
	Total int           `json:"total"`
}

// SessionItem is a session list entry.
type SessionItem struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	AgentType string `json:"agent_type"`
	UpdatedAt string `json:"updated_at"`
}

// GetSessionMessagesReq returns messages for a session.
type GetSessionMessagesReq struct {
	g.Meta    `path:"/sessions/{session_id}/messages" method:"get" tags:"Session" summary:"获取会话消息"`
	SessionID string `json:"session_id" in:"path" v:"required"`
}

// GetSessionMessagesRes returns session message history.
type GetSessionMessagesRes struct {
	SessionID string          `json:"session_id"`
	Messages  []MessageItem   `json:"messages"`
}

// MessageItem is a single chat message.
type MessageItem struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}
