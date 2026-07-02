package handler

import (
	"context"
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
	".md":        {},
	".txt":       {},
	".markdown":  {},
}

// UploadDocument stores a file and runs synchronous indexing.
func (c *ControllerV1) UploadDocument(ctx context.Context, req *v1.UploadDocumentReq) (*v1.UploadDocumentRes, error) {
	if c.app.RAG == nil {
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

	content, err := io.ReadAll(reader)
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
		return nil, err
	}

	task, err := c.app.RAG.GetIndexTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	return &v1.UploadDocumentRes{
		DocID:    docID,
		TaskID:   taskID,
		FileName: filename,
		FileSize: int64(len(content)),
		Status:   string(task.Status),
	}, nil
}

// ListDocuments returns paginated knowledge documents.
func (c *ControllerV1) ListDocuments(ctx context.Context, req *v1.ListDocumentsReq) (*v1.ListDocumentsRes, error) {
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

// DeleteDocument soft-deletes a document and removes Milvus chunks.
func (c *ControllerV1) DeleteDocument(ctx context.Context, req *v1.DeleteDocumentReq) (*v1.DeleteDocumentRes, error) {
	tenantID := ctxkeys.TenantIDFrom(ctx)
	doc, err := c.app.Documents.Get(ctx, tenantID, req.DocID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if doc == nil || doc.Status == "deleted" {
		return nil, apperr.ErrNotFound
	}

	if err := c.app.Documents.SoftDelete(ctx, tenantID, req.DocID); err != nil {
		return nil, apperr.Wrap(err, apperr.ErrInternal)
	}
	if c.app.RAG != nil {
		if err := c.app.RAG.DeleteDocumentChunks(ctx, req.DocID); err != nil {
			return nil, apperr.Wrap(err, apperr.ErrRAGFailed)
		}
	}

	return &v1.DeleteDocumentRes{DocID: req.DocID, Status: "deleted"}, nil
}

// GetIndexTask returns indexing task status.
func (c *ControllerV1) GetIndexTask(ctx context.Context, req *v1.GetIndexTaskReq) (*v1.GetIndexTaskRes, error) {
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
