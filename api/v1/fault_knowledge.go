package v1

import "github.com/gogf/gf/v2/frame/g"

// ListFaultKnowledgeReq lists generated troubleshooting knowledge cards.
type ListFaultKnowledgeReq struct {
	g.Meta `path:"/knowledge/fault-cards" method:"get" tags:"Knowledge" summary:"故障知识卡片列表"`
	Page   int    `json:"page" d:"1" in:"query"`
	Size   int    `json:"size" d:"20" in:"query"`
	Status string `json:"status" in:"query"`
}

type ListFaultKnowledgeRes struct {
	Items []FaultKnowledgeItem `json:"items"`
	Total int                  `json:"total"`
}

type FaultKnowledgeItem struct {
	CardID      string  `json:"card_id"`
	TaskID      string  `json:"task_id"`
	TraceID     string  `json:"trace_id"`
	Title       string  `json:"title"`
	Symptom     string  `json:"symptom"`
	Impact      string  `json:"impact"`
	RootCause   string  `json:"root_cause"`
	Workaround  string  `json:"workaround"`
	Remediation string  `json:"remediation"`
	Service     string  `json:"service"`
	Version     string  `json:"version"`
	Status      string  `json:"status"`
	Weight      float64 `json:"weight"`
	DocID       string  `json:"doc_id"`
	HitCount    int     `json:"hit_count"`
	UsefulCount int     `json:"useful_count"`
	BadCount    int     `json:"bad_count"`
	CreatedBy   string  `json:"created_by"`
	ReviewedBy  string  `json:"reviewed_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	ReviewedAt  string  `json:"reviewed_at,omitempty"`
}

// ApproveFaultKnowledgeReq approves a draft card and indexes it as fault_case knowledge.
type ApproveFaultKnowledgeReq struct {
	g.Meta `path:"/knowledge/fault-cards/{card_id}/approve" method:"post" tags:"Knowledge" summary:"审核通过故障知识卡片"`
	CardID string `json:"card_id" in:"path" v:"required"`
}

type ApproveFaultKnowledgeRes struct {
	CardID string `json:"card_id"`
	DocID  string `json:"doc_id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// RejectFaultKnowledgeReq rejects a generated draft card.
type RejectFaultKnowledgeReq struct {
	g.Meta `path:"/knowledge/fault-cards/{card_id}/reject" method:"post" tags:"Knowledge" summary:"驳回故障知识卡片"`
	CardID string `json:"card_id" in:"path" v:"required"`
}

type RejectFaultKnowledgeRes struct {
	CardID string `json:"card_id"`
	Status string `json:"status"`
}

// FeedbackFaultKnowledgeReq records user feedback and adjusts card weight.
type FeedbackFaultKnowledgeReq struct {
	g.Meta  `path:"/knowledge/fault-cards/{card_id}/feedback" method:"post" tags:"Knowledge" summary:"故障知识反馈"`
	CardID  string `json:"card_id" in:"path" v:"required"`
	Rating  string `json:"rating" v:"required#rating required"`
	Comment string `json:"comment"`
}

type FeedbackFaultKnowledgeRes struct {
	CardID string `json:"card_id"`
	Rating string `json:"rating"`
	Status string `json:"status"`
}
