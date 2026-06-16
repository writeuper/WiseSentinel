package v1

import "github.com/gogf/gf/v2/frame/g"

// UploadDocumentReq uploads a knowledge document.
type UploadDocumentReq struct {
	g.Meta      `path:"/knowledge/documents/upload" method:"post" mime:"multipart/form-data" tags:"Knowledge" summary:"上传文档并索引"`
	Visibility  string `json:"visibility" d:"tenant"`
	SecretLevel int    `json:"secret_level" d:"1"`
}

// UploadDocumentRes returns upload and index task info.
type UploadDocumentRes struct {
	DocID    string `json:"doc_id"`
	TaskID   string `json:"task_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	Status   string `json:"status"`
}

// ListDocumentsReq lists knowledge documents.
type ListDocumentsReq struct {
	g.Meta `path:"/knowledge/documents" method:"get" tags:"Knowledge" summary:"文档列表"`
	Page   int    `json:"page" d:"1" in:"query"`
	Size   int    `json:"size" d:"20" in:"query"`
	Status string `json:"status" in:"query"`
}

// ListDocumentsRes returns paginated documents.
type ListDocumentsRes struct {
	Items []DocumentItem `json:"items"`
	Total int            `json:"total"`
}

// DocumentItem is a document list entry.
type DocumentItem struct {
	DocID      string `json:"doc_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Visibility string `json:"visibility"`
	UpdatedAt  string `json:"updated_at"`
}

// DeleteDocumentReq soft-deletes a document.
type DeleteDocumentReq struct {
	g.Meta `path:"/knowledge/documents/{doc_id}" method:"delete" tags:"Knowledge" summary:"删除文档"`
	DocID  string `json:"doc_id" in:"path" v:"required"`
}

// DeleteDocumentRes confirms deletion.
type DeleteDocumentRes struct {
	DocID  string `json:"doc_id"`
	Status string `json:"status"`
}

// GetIndexTaskReq queries index task status.
type GetIndexTaskReq struct {
	g.Meta `path:"/knowledge/index-tasks/{task_id}" method:"get" tags:"Knowledge" summary:"查询索引任务"`
	TaskID string `json:"task_id" in:"path" v:"required"`
}

// GetIndexTaskRes returns index task status.
type GetIndexTaskRes struct {
	TaskID     string `json:"task_id"`
	DocID      string `json:"doc_id"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
	ErrorMsg   string `json:"error_msg,omitempty"`
}
