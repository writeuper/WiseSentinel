package v1

import "github.com/gogf/gf/v2/frame/g"

type ListVectorGCTasksReq struct {
	g.Meta `path:"/admin/vector-gc/tasks" method:"get" tags:"Admin" summary:"向量 GC 任务列表"`
	Status string `json:"status" in:"query" v:"in:pending,running,retry_wait,succeeded,skipped,dead"`
	Page   int    `json:"page" d:"1" in:"query"`
	Size   int    `json:"size" d:"20" in:"query"`
}

type VectorGCTaskItem struct {
	DocID            string `json:"doc_id"`
	TargetKey        string `json:"target_key"`
	TargetKind       string `json:"target_kind"`
	TargetGeneration uint64 `json:"target_generation"`
	Status           string `json:"status"`
	AttemptCount     int    `json:"attempt_count"`
	MaxAttempts      int    `json:"max_attempts"`
	LastError        string `json:"last_error,omitempty"`
	NextAttemptAt    string `json:"next_attempt_at,omitempty"`
}

type ListVectorGCTasksRes struct {
	Items []VectorGCTaskItem `json:"items"`
	Total int                `json:"total"`
}

type RequestVectorGCRedriveReq struct {
	g.Meta    `path:"/admin/vector-gc/documents/{doc_id}/tasks/{target_key}/redrive" method:"post" tags:"Admin" summary:"申请重驱死信向量 GC"`
	DocID     string `json:"doc_id" in:"path" v:"required"`
	TargetKey string `json:"target_key" in:"path" v:"required"`
	Reason    string `json:"reason" v:"required-length:1,500"`
}

type RequestVectorGCRedriveRes struct {
	ApprovalID string `json:"approval_id"`
	Status     string `json:"status"`
}
