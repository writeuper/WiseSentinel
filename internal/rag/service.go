package rag

import (
	"context"
	"sort"
	"strings"
	"time"
	"wisesentinel-platform/internal/agent/knowledge"
	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
	"wisesentinel-platform/internal/pkg/redact"
	"wisesentinel-platform/internal/rag/indexer"
	"wisesentinel-platform/internal/rag/retriever"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// Service implements domain.RAGService.
type Service struct {
	pipeline  *knowledge.Pipeline
	retriever documentRetriever
	indexer   *indexer.MilvusIndexer
	tasks     *repository.IndexTaskRepo
	states    *repository.DocumentIndexStateRepo
}

type documentRetriever interface {
	Retrieve(context.Context, *domain.RetrieveRequest) (*domain.RetrieveResponse, error)
}

// NewService wires the RAG engine components.
func NewService(
	pipeline *knowledge.Pipeline,
	retriever *retriever.MilvusRetriever,
	indexer *indexer.MilvusIndexer,
	tasks *repository.IndexTaskRepo,
) *Service {
	service := &Service{
		pipeline:  pipeline,
		retriever: retriever,
		indexer:   indexer,
		tasks:     tasks,
		states:    repository.NewDocumentIndexStateRepo(),
	}
	if retriever != nil {
		retriever.SetActiveGenerationResolver(service.states)
	}
	return service
}

func (s *Service) Ready() bool {
	return s != nil && s.pipeline != nil && s.retriever != nil && s.indexer != nil && s.tasks != nil && s.states != nil
}

// Retrieve performs tenant-filtered vector search.
func (s *Service) Retrieve(ctx context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	startedAt := time.Now()
	outcome, confidence := "error", string(domain.ConfidenceLow)
	defer func() { observability.ObserveRAGRetrieve(outcome, confidence, time.Since(startedAt).Seconds()) }()
	if s.retriever == nil {
		return nil, apperr.ErrRAGFailed
	}
	if req == nil {
		return nil, apperr.ErrBadRequest
	}
	if req.MaxSecretLevel <= 0 {
		req = cloneRetrieveRequest(req)
		req.MaxSecretLevel = domain.MaxSecretLevelForRoles(ctxkeys.RolesFrom(ctx))
	}
	resp, err := s.retrieveMulti(ctx, req)
	if err != nil {
		return nil, apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	classifyRetrieveConfidence(resp)
	outcome = "success"
	if resp != nil {
		confidence = string(resp.Confidence)
	}
	return resp, nil
}

func (s *Service) retrieveMulti(ctx context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	queries, expanded := buildRetrievalQueries(req)
	if len(queries) == 0 {
		return &domain.RetrieveResponse{Confidence: domain.ConfidenceLow}, nil
	}
	merged := make(map[string]domain.RetrievedDocument)
	for _, query := range queries {
		current := cloneRetrieveRequest(req)
		current.Query = query
		current.QueryVariants = nil
		current.EnableQueryExpansion = false
		resp, err := s.retriever.Retrieve(ctx, current)
		if err != nil {
			if strings.TrimSpace(query) == strings.TrimSpace(req.Query) {
				return nil, err
			}
			continue
		}
		if resp == nil {
			continue
		}
		for _, doc := range resp.Documents {
			key := doc.ChunkID
			if key == "" {
				key = doc.DocID + "\x00" + doc.Source + "\x00" + doc.Content
			}
			if old, ok := merged[key]; !ok || doc.Score > old.Score {
				merged[key] = doc
			}
		}
	}
	docs := make([]domain.RetrievedDocument, 0, len(merged))
	for _, doc := range merged {
		docs = append(docs, doc)
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Score > docs[j].Score })
	if req.TopK > 0 && len(docs) > req.TopK {
		docs = docs[:req.TopK]
	}
	resp := &domain.RetrieveResponse{Documents: docs, QueryCount: len(queries), Expanded: expanded}
	classifyRetrieveConfidence(resp)
	return resp, nil
}

func (s *Service) Route(ctx context.Context, req *domain.RetrieveRequest) (*domain.RAGRouteDecision, error) {
	resp, err := s.Retrieve(ctx, req)
	if err != nil {
		return nil, err
	}
	decision := &domain.RAGRouteDecision{
		Response:   resp,
		Confidence: domain.ConfidenceLow,
		Route:      "realtime_tools",
		Reason:     "no reliable knowledge hit, use realtime tools",
	}
	if resp != nil {
		decision.Confidence = resp.Confidence
		switch resp.Confidence {
		case domain.ConfidenceHigh:
			decision.Route = "fast_answer"
			decision.Reason = "high confidence knowledge hit"
		case domain.ConfidenceMid:
			decision.Route = "agent_rag"
			decision.Reason = "medium confidence knowledge hit, use agent with RAG"
		default:
			decision.Route = "realtime_tools"
			decision.Reason = "low confidence knowledge hit, prefer realtime troubleshooting tools"
		}
	}
	return decision, nil
}

func classifyRetrieveConfidence(resp *domain.RetrieveResponse) {
	if resp == nil {
		return
	}
	for _, doc := range resp.Documents {
		if doc.Score > resp.TopScore {
			resp.TopScore = doc.Score
		}
	}
	switch {
	case resp.TopScore >= 0.75:
		resp.Confidence = domain.ConfidenceHigh
	case resp.TopScore >= 0.45:
		resp.Confidence = domain.ConfidenceMid
	default:
		resp.Confidence = domain.ConfidenceLow
	}
}

func cloneRetrieveRequest(req *domain.RetrieveRequest) *domain.RetrieveRequest {
	if req == nil {
		return nil
	}
	copied := *req
	return &copied
}

// SubmitIndexTask creates a pending index task. IndexWorker executes it asynchronously.
func (s *Service) SubmitIndexTask(ctx context.Context, req *domain.IndexTaskRequest) (string, error) {
	if s.tasks == nil || s.states == nil || s.pipeline == nil || s.indexer == nil {
		return "", apperr.ErrRAGFailed
	}
	if req == nil {
		return "", apperr.ErrBadRequest
	}
	taskID := uuid.NewString()
	record := &repository.IndexTaskRecord{
		TenantID:    req.TenantID,
		TaskID:      taskID,
		DocID:       req.DocID,
		SourceURI:   req.SourceURI,
		Visibility:  req.Visibility,
		SecretLevel: req.SecretLevel,
		Layer:       req.Layer,
		Version:     req.Version,
		Service:     req.Service,
		Status:      string(domain.IndexTaskPending),
	}
	if err := s.states.AllocateAndCreateTask(ctx, record); err != nil {
		return "", apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	return taskID, nil
}

// ExecuteIndexTask executes an existing persisted index task.
func (s *Service) ExecuteIndexTask(ctx context.Context, tenantID, taskID, executionToken string) error {
	if s.pipeline == nil || s.indexer == nil || s.tasks == nil || s.states == nil {
		return apperr.ErrRAGFailed
	}
	record, err := s.tasks.Get(ctx, tenantID, taskID)
	if err != nil {
		return err
	}
	if record == nil {
		return apperr.ErrNotFound
	}
	if record.Status != string(domain.IndexTaskRunning) || record.ExecutionToken != executionToken || record.LeaseExpiresAt == nil || !record.LeaseExpiresAt.After(time.Now()) {
		return apperr.ErrRAGFailed
	}
	chunkCount, err := s.pipeline.IndexDocument(ctx, &domain.IndexTaskRequest{
		TaskID:      record.TaskID,
		Generation:  record.Generation,
		TenantID:    record.TenantID,
		DocID:       record.DocID,
		SourceURI:   record.SourceURI,
		Visibility:  record.Visibility,
		SecretLevel: record.SecretLevel,
		Layer:       domain.KnowledgeLayer(record.Layer),
		Version:     record.Version,
		Service:     record.Service,
	})
	if err != nil {
		safeSource := redact.Summary(record.SourceURI, 500)
		safeErr := redact.Summary(err.Error(), 1000)
		g.Log().Errorf(ctx, "RAG index task failed: task_id=%s doc_id=%s source=%s cause=%s", taskID, record.DocID, safeSource, safeErr)
		// The IndexWorker owns the terminal transition. It classifies transient
		// dependency failures into retry_wait and only marks non-retryable
		// failures terminal; doing a failed CAS here would prevent durable retry.
		return apperr.Wrap(err, apperr.ErrRAGFailed)
	}
	published, err := s.states.PublishIfOwned(ctx, tenantID, taskID, executionToken, chunkCount)
	if err != nil {
		return err
	}
	if !published {
		// Staged vectors remain unreadable until a later GC pass. If this worker
		// still owns the task, record the supersession; a lost lease simply wins
		// the CAS and cannot be compensated by deleting vectors.
		marked, markErr := s.tasks.MarkFinishedIfOwned(ctx, tenantID, taskID, executionToken, string(domain.IndexTaskFailed), 0, "index generation superseded before publication")
		if markErr != nil {
			return apperr.Wrap(markErr, apperr.ErrRAGFailed)
		}
		if !marked {
			return apperr.ErrConflict
		}
	}
	return nil
}

// GetIndexTask returns index task status from MySQL.
func (s *Service) GetIndexTask(ctx context.Context, tenantID, taskID string) (*domain.IndexTask, error) {
	if s.tasks == nil {
		return nil, apperr.ErrRAGFailed
	}
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

// DeleteDocumentChunks removes all vectors for a document within its tenant.
func (s *Service) DeleteDocumentChunks(ctx context.Context, tenantID, docID string) error {
	if s.indexer == nil {
		return apperr.ErrRAGFailed
	}
	return s.indexer.DeleteByDocID(ctx, tenantID, docID)
}

// CancelActiveIndexTasks prevents deleted documents from being indexed later.
func (s *Service) CancelActiveIndexTasks(ctx context.Context, tenantID, docID string) error {
	if s.tasks == nil {
		return apperr.ErrRAGFailed
	}
	return s.tasks.CancelActive(ctx, tenantID, docID)
}

var _ domain.RAGService = (*Service)(nil)
var _ domain.IndexTaskExecutor = (*Service)(nil)
