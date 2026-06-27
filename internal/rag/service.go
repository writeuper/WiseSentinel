package rag

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/agent/knowledge"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/repository"

	"github.com/google/uuid"
)

// Service implements domain.RAGService.
type Service struct {
	pipeline  *knowledge.Pipeline
	retriever *retriever.MilvusRetriever
	indexer   *indexer.MilvusIndexer
	tasks     *repository.IndexTaskRepo
}

// NewService wires the RAG engine components.
func NewService(
	pipeline *knowledge.Pipeline,
	retriever *retriever.MilvusRetriever,
	indexer *indexer.MilvusIndexer,
	tasks *repository.IndexTaskRepo,
) *Service {
	return &Service{
		pipeline:  pipeline,
		retriever: retriever,
		indexer:   indexer,
		tasks:     tasks,
	}
}

// Retrieve performs tenant-filtered vector search.
func (s *Service) Retrieve(ctx context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	if s.retriever == nil {
		return nil, apperr.ErrRAGFailed
	}
	resp, err := s.retriever.Retrieve(ctx, req)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	return resp, nil
}

// SubmitIndexTask runs synchronous indexing for Phase 1.
func (s *Service) SubmitIndexTask(ctx context.Context, req *domain.IndexTaskRequest) (string, error) {
	if s.pipeline == nil || s.tasks == nil {
		return "", apperr.ErrRAGFailed
	}

	taskID := uuid.NewString()
	record := &repository.IndexTaskRecord{
		TenantID: req.TenantID,
		TaskID:   taskID,
		DocID:    req.DocID,
		Status:   string(domain.IndexTaskPending),
	}
	if err := s.tasks.Create(ctx, record); err != nil {
		return "", apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	if err := s.tasks.MarkRunning(ctx, req.TenantID, taskID); err != nil {
		return "", apperr.Wrap(err, apperr.ErrRAGFailed)
	}

	chunkCount, err := s.pipeline.IndexDocument(ctx, req)
	if err != nil {
		_ = s.tasks.MarkFinished(ctx, req.TenantID, taskID, string(domain.IndexTaskFailed), 0, err.Error())
		return taskID, apperr.Wrap(err, apperr.ErrRAGFailed)
	}

	if err := s.tasks.MarkFinished(ctx, req.TenantID, taskID, string(domain.IndexTaskSuccess), chunkCount, ""); err != nil {
		return taskID, apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	return taskID, nil
}

// GetIndexTask returns index task status from MySQL.
func (s *Service) GetIndexTask(ctx context.Context, tenantID, taskID string) (*domain.IndexTask, error) {
	record, err := s.tasks.Get(ctx, tenantID, taskID)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	if record == nil {
		return nil, apperr.ErrNotFound
	}
	return &domain.IndexTask{
		TaskID:     record.TaskID,
		DocID:      record.DocID,
		Status:     domain.IndexTaskStatus(record.Status),
		ChunkCount: record.ChunkCount,
		ErrorMsg:   record.ErrorMsg,
	}, nil
}

// DeleteDocumentChunks removes vectors for a document.
func (s *Service) DeleteDocumentChunks(ctx context.Context, docID string) error {
	if s.indexer == nil {
		return fmt.Errorf("indexer is nil")
	}
	return s.indexer.DeleteByDocID(ctx, docID)
}
