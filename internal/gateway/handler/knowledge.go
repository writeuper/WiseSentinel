package handler

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	v1 "wisesentinel-platform/api/v1"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/repository"

	"github.com/google/uuid"
)

const maxUploadBytes = 50 << 20 // 50MB

var allowedUploadExt = map[string]struct{}{
	".md":       {},
	".txt":      {},
	".markdown": {},
}

// UploadDocument stores a file and runs synchronous indexing.
func (c *ControllerV1) UploadDocument(ctx context.Context, req *v1.UploadDocumentReq) (*v1.UploadDocumentRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	if c.app.RAG == nil {
		return nil, apperr.ErrRAGFailed
	}
	if ready, ok := c.app.RAG.(interface{ Ready() bool }); ok && !ready.Ready() {
		return nil, apperr.ErrRAGFailed
	}
	if req.File == nil {
		return nil, apperr.ErrBadRequest
	}

	filename := filepath.Base(req.File.Filename)
	ext := strings.ToLower(filepath.Ext(filename))
	if _, ok := allowedUploadExt[ext]; !ok {
		return nil, apperr.New(40001, 400, "仅支持 .md / .txt / .markdown 文件")
	}
	if req.File.Size > maxUploadBytes {
		return nil, apperr.New(40001, 400, "文件大小不能超过 50MB")
	}

	reader, err := req.File.Open()
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrBadRequest)
	}
	defer reader.Close()

	content, err := readBoundedUpload(reader, maxUploadBytes)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrBadRequest)
	}

	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	docID := uuid.NewString()

	sourceURI, err := c.app.Storage.Save(tenantID, docID, filename, content)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	visibility := req.Visibility
	if visibility == "" {
		visibility = "tenant"
	}
	secretLevel := req.SecretLevel
	if secretLevel <= 0 {
		secretLevel = domain.SecretLevelInternal
	}

	doc := &repository.Document{
		TenantID:    tenantID,
		DocID:       docID,
		Name:        filename,
		SourceURI:   sourceURI,
		MimeType:    mimeTypeForExt(ext),
		Visibility:  visibility,
		SecretLevel: secretLevel,
		Status:      "active",
		CreatedBy:   userID,
	}
	if err := c.app.Documents.Create(ctx, doc); err != nil {
		_ = c.app.Storage.Remove(sourceURI)
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	taskID, err := c.app.RAG.SubmitIndexTask(ctx, &domain.IndexTaskRequest{
		TenantID:    tenantID,
		DocID:       docID,
		SourceURI:   sourceURI,
		Visibility:  visibility,
		SecretLevel: secretLevel,
	})
	if err != nil {
		_ = c.app.Documents.SoftDelete(ctx, tenantID, docID)
		return nil, err
	}

	return &v1.UploadDocumentRes{
		DocID:    docID,
		TaskID:   taskID,
		FileName: filename,
		FileSize: int64(len(content)),
		Status:   string(domain.IndexTaskPending),
	}, nil
}

func readBoundedUpload(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil || maxBytes <= 0 {
		return nil, fmt.Errorf("invalid upload reader or limit")
	}
	content, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("文件大小不能超过 %d 字节", maxBytes)
	}
	return content, nil
}

// ListDocuments returns paginated knowledge documents.
func (c *ControllerV1) ListDocuments(ctx context.Context, req *v1.ListDocumentsReq) (*v1.ListDocumentsRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	docs, total, err := c.app.Documents.List(ctx, tenantID, req.Status, req.Page, req.Size)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	items := make([]v1.DocumentItem, len(docs))
	for i, doc := range docs {
		items[i] = v1.DocumentItem{
			DocID:      doc.DocID,
			Name:       doc.Name,
			Status:     doc.Status,
			Visibility: doc.Visibility,
			UpdatedAt:  doc.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}
	return &v1.ListDocumentsRes{Items: items, Total: total}, nil
}

// DeleteDocument soft-deletes a document and durably schedules tenant-scoped
// vector cleanup. Retrieval fails closed immediately; Milvus cleanup retries.
func (c *ControllerV1) DeleteDocument(ctx context.Context, req *v1.DeleteDocumentReq) (*v1.DeleteDocumentRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	doc, err := c.app.Documents.Get(ctx, tenantID, req.DocID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if doc == nil || doc.Status == "deleted" {
		return nil, apperr.ErrNotFound
	}

	// Make the document lifecycle and durable cleanup intent commit together.
	// PublishIfOwned checks this state, and the retriever resolves only active
	// documents, so a concurrent worker can no longer publish a readable vector.
	if err := c.app.Documents.SoftDeleteAndEnqueueVectorGC(ctx, tenantID, req.DocID); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}

	return &v1.DeleteDocumentRes{DocID: req.DocID, Status: "deleted"}, nil
}

// ReindexDocument schedules a generation-fenced rebuild of an active document.
// It never deletes the current readable vectors on the request path: the index
// worker stages a new generation, then atomically publishes it only if the
// document remains active and the request is still the desired generation.
func (c *ControllerV1) ReindexDocument(ctx context.Context, req *v1.ReindexDocumentReq) (*v1.ReindexDocumentRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	if c.app.RAG == nil {
		return nil, apperr.ErrRAGFailed
	}
	if ready, ok := c.app.RAG.(interface{ Ready() bool }); ok && !ready.Ready() {
		return nil, apperr.ErrRAGFailed
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	doc, err := c.app.Documents.Get(ctx, tenantID, req.DocID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if doc == nil || doc.Status != "active" {
		return nil, apperr.ErrNotFound
	}
	taskID, err := c.app.RAG.SubmitIndexTask(ctx, &domain.IndexTaskRequest{
		TenantID:    tenantID,
		DocID:       doc.DocID,
		SourceURI:   doc.SourceURI,
		Visibility:  doc.Visibility,
		SecretLevel: doc.SecretLevel,
	})
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	return &v1.ReindexDocumentRes{DocID: doc.DocID, TaskID: taskID, Status: string(domain.IndexTaskPending)}, nil
}

// GetIndexTask returns indexing task status.
func (c *ControllerV1) GetIndexTask(ctx context.Context, req *v1.GetIndexTaskReq) (*v1.GetIndexTaskRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	if c.app.RAG == nil {
		return nil, apperr.ErrRAGFailed
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	task, err := c.app.RAG.GetIndexTask(ctx, tenantID, req.TaskID)
	if err != nil {
		return nil, err
	}
	return &v1.GetIndexTaskRes{
		TaskID:     task.TaskID,
		DocID:      task.DocID,
		Status:     string(task.Status),
		ChunkCount: task.ChunkCount,
		ErrorMsg:   task.ErrorMsg,
	}, nil
}

func (c *ControllerV1) ListFaultKnowledge(ctx context.Context, req *v1.ListFaultKnowledgeReq) (*v1.ListFaultKnowledgeRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	rows, total, err := c.app.FaultKnowledgeRepo.List(ctx, tenantID, req.Status, req.Page, req.Size)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	items := make([]v1.FaultKnowledgeItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, toFaultKnowledgeItem(row))
	}
	return &v1.ListFaultKnowledgeRes{Items: items, Total: total}, nil
}

func (c *ControllerV1) ApproveFaultKnowledge(ctx context.Context, req *v1.ApproveFaultKnowledgeReq) (*v1.ApproveFaultKnowledgeRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	if c.app.RAG == nil {
		return nil, apperr.ErrRAGFailed
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	card, err := c.app.FaultKnowledgeRepo.Get(ctx, tenantID, req.CardID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if card == nil {
		return nil, apperr.ErrNotFound
	}
	content := renderFaultKnowledgeMarkdown(card)
	docID := "fkdoc_" + uuid.NewString()
	filename := req.CardID + ".md"
	sourceURI, err := c.app.Storage.Save(tenantID, docID, filename, []byte(content))
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	doc := &repository.Document{
		TenantID:    tenantID,
		DocID:       docID,
		Name:        "故障知识卡片 - " + card.Title,
		SourceURI:   sourceURI,
		MimeType:    "text/markdown",
		Visibility:  "tenant",
		SecretLevel: domain.SecretLevelInternal,
		Status:      "active",
		CreatedBy:   userID,
	}
	if err := c.app.Documents.Create(ctx, doc); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	taskID, err := c.app.RAG.SubmitIndexTask(ctx, &domain.IndexTaskRequest{
		TenantID:    tenantID,
		DocID:       docID,
		SourceURI:   sourceURI,
		Visibility:  "tenant",
		SecretLevel: domain.SecretLevelInternal,
		Layer:       domain.KnowledgeLayerFaultCase,
		Version:     card.Version,
		Service:     card.Service,
	})
	if err != nil {
		return nil, err
	}
	if err := c.app.FaultKnowledgeRepo.Approve(ctx, tenantID, req.CardID, userID, docID); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	return &v1.ApproveFaultKnowledgeRes{CardID: req.CardID, DocID: docID, TaskID: taskID, Status: "approved"}, nil
}

func (c *ControllerV1) RejectFaultKnowledge(ctx context.Context, req *v1.RejectFaultKnowledgeReq) (*v1.RejectFaultKnowledgeRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	userID := ctxkeys.UserIDFrom(ctx)
	if err := c.app.FaultKnowledgeRepo.Reject(ctx, tenantID, req.CardID, userID); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	return &v1.RejectFaultKnowledgeRes{CardID: req.CardID, Status: "rejected"}, nil
}

func (c *ControllerV1) FeedbackFaultKnowledge(ctx context.Context, req *v1.FeedbackFaultKnowledgeReq) (*v1.FeedbackFaultKnowledgeRes, error) {
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	if req.Rating != "useful" && req.Rating != "bad" {
		return nil, apperr.New(40001, 400, "rating must be useful or bad")
	}
	if err := validateBoundedText(req.Comment, maxCommentRunes); err != nil {
		return nil, err
	}
	tenantID := ctxkeys.TenantIDFrom(ctx)
	feedbackID := "fb_" + uuid.NewString()
	if err := c.app.FeedbackRepo.Create(ctx, &repository.Feedback{
		TenantID:   tenantID,
		FeedbackID: feedbackID,
		TargetType: "fault_knowledge",
		TargetID:   req.CardID,
		Rating:     req.Rating,
		Comment:    req.Comment,
		CreatedBy:  ctxkeys.UserIDFrom(ctx),
	}); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if err := c.app.FaultKnowledgeRepo.Feedback(ctx, tenantID, req.CardID, req.Rating == "useful"); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	return &v1.FeedbackFaultKnowledgeRes{CardID: req.CardID, Rating: req.Rating, Status: "recorded"}, nil
}

func renderFaultKnowledgeMarkdown(card *repository.FaultKnowledge) string {
	return fmt.Sprintf(`# %s

## 故障现象
%s

## 影响范围
%s

## 根因判断
%s

## 临时止血
%s

## 根治建议
%s

## 元数据
- service: %s
- version: %s
- task_id: %s
- trace_id: %s
`, card.Title, card.Symptom, card.Impact, card.RootCause, card.Workaround, card.Remediation, card.Service, card.Version, card.TaskID, card.TraceID)
}

func toFaultKnowledgeItem(row repository.FaultKnowledge) v1.FaultKnowledgeItem {
	reviewedAt := ""
	if row.ReviewedAt != nil && !row.ReviewedAt.IsZero() {
		reviewedAt = row.ReviewedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return v1.FaultKnowledgeItem{
		CardID:      row.CardID,
		TaskID:      row.TaskID,
		TraceID:     row.TraceID,
		Title:       row.Title,
		Symptom:     row.Symptom,
		Impact:      row.Impact,
		RootCause:   row.RootCause,
		Workaround:  row.Workaround,
		Remediation: row.Remediation,
		Service:     row.Service,
		Version:     row.Version,
		Status:      row.Status,
		Weight:      row.Weight,
		DocID:       row.DocID,
		HitCount:    row.HitCount,
		UsefulCount: row.UsefulCount,
		BadCount:    row.BadCount,
		CreatedBy:   row.CreatedBy,
		ReviewedBy:  row.ReviewedBy,
		CreatedAt:   row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   row.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		ReviewedAt:  reviewedAt,
	}
}

func mimeTypeForExt(ext string) string {
	switch ext {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}
