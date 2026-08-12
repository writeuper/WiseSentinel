package v1

import "github.com/gogf/gf/v2/frame/g"

// ListApprovalsReq lists pending approvals.
type ListApprovalsReq struct {
	g.Meta `path:"/approvals" method:"get" tags:"Approval" summary:"待审批列表"`
	Page   int `json:"page" d:"1" in:"query"`
	Size   int `json:"size" d:"20" in:"query"`
}

// ListApprovalsRes returns approval items.
type ListApprovalsRes struct {
	Items []ApprovalItem `json:"items"`
	Total int            `json:"total"`
}

// ApprovalItem is an approval queue entry.
type ApprovalItem struct {
	ApprovalID   string          `json:"approval_id"`
	TaskID       string          `json:"task_id"`
	ApprovalType string          `json:"approval_type"`
	Status       string          `json:"status"`
	ExpiredAt    string          `json:"expired_at"`
	Target       *ApprovalTarget `json:"target,omitempty"`
}

// ApprovalTarget is a minimal, server-projected target description. It is not
// a copy of the approval payload and intentionally omits requester, reason,
// credentials, and arbitrary tool arguments.
type ApprovalTarget struct {
	Kind      string `json:"kind"`
	DocID     string `json:"doc_id,omitempty"`
	TargetKey string `json:"target_key,omitempty"`
}

// ApprovalDecisionReq submits an approval decision.
type ApprovalDecisionReq struct {
	g.Meta     `path:"/approvals/{approval_id}/decision" method:"post" tags:"Approval" summary:"审批决策"`
	ApprovalID string `json:"approval_id" in:"path" v:"required"`
	Decision   string `json:"decision" v:"required|in:approved,rejected"`
	Comment    string `json:"comment"`
}

// ApprovalDecisionRes confirms the decision.
type ApprovalDecisionRes struct {
	ApprovalID string `json:"approval_id"`
	Status     string `json:"status"`
}

// ListAgentConfigsReq lists agent configuration versions.
type ListAgentConfigsReq struct {
	g.Meta    `path:"/admin/agent-configs" method:"get" tags:"Admin" summary:"Agent 配置列表"`
	AgentType string `json:"agent_type" in:"query" v:"required"`
}

// ListAgentConfigsRes returns config versions.
type ListAgentConfigsRes struct {
	Items []AgentConfigItem `json:"items"`
}

// AgentConfigItem is an agent config version entry.
type AgentConfigItem struct {
	AgentType string `json:"agent_type"`
	Version   string `json:"version"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
}

// ActivateAgentConfigReq activates a config version.
type ActivateAgentConfigReq struct {
	g.Meta    `path:"/admin/agent-configs/{version}/activate" method:"put" tags:"Admin" summary:"激活 Agent 配置"`
	Version   string `json:"version" in:"path" v:"required"`
	AgentType string `json:"agent_type" in:"query" v:"required"`
}

// ActivateAgentConfigRes confirms activation.
type ActivateAgentConfigRes struct {
	AgentType string `json:"agent_type"`
	Version   string `json:"version"`
	IsActive  bool   `json:"is_active"`
}
