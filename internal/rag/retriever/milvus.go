package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/filter"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	defaultTopK = 3
	// l2RefDistance is the L2 distance at which similarity drops to 0.
	// Embedding vectors are typically L2-normalized; for such vectors the
	// maximum L2 distance between two unit vectors is 2.0. We map distance
	// d ∈ [0,2] to similarity = 1 - d/2, so distance 0 → score 1.0 and
	// distance 2 → score 0.0. For non-normalized embeddings this is still
	// a monotonic, bounded approximation sufficient for routing decisions.
	l2RefDistance = 2.0
	// highConfThreshold and midConfThreshold mirror §5.2 / §6.1 of the
	// refactor design doc.
	highConfThreshold             = 0.75
	midConfThreshold              = 0.5
	generationCandidateMultiplier = 5
	maxGenerationCandidateTopK    = 100
)

// ActiveGenerationResolver resolves which vector generation is readable for
// each document. MySQL is the authority; Milvus metadata alone is not enough.
type ActiveGenerationResolver interface {
	ResolveActiveGenerations(ctx context.Context, tenantID string, docIDs []string) (map[string]domain.DocumentIndexGeneration, error)
}

// MilvusRetriever performs vector search with tenant filters.
type MilvusRetriever struct {
	milvus   *client.MilvusClient
	embedder embedder.Embedder
	resolver ActiveGenerationResolver
}

func NewMilvusRetriever(mc *client.MilvusClient, emb embedder.Embedder) *MilvusRetriever {
	return &MilvusRetriever{milvus: mc, embedder: emb}
}

// SetActiveGenerationResolver enables fail-closed filtering of staged and
// superseded generations after Milvus candidate search.
func (r *MilvusRetriever) SetActiveGenerationResolver(resolver ActiveGenerationResolver) {
	if r != nil {
		r.resolver = resolver
	}
}

// Retrieve searches Milvus, normalizes scores, enriches metadata, and computes
// a coarse-grained confidence level used by the orchestrator for path routing.
func (r *MilvusRetriever) Retrieve(ctx context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("retrieve request is nil")
	}
	if r == nil || r.milvus == nil {
		return nil, fmt.Errorf("milvus client is nil")
	}
	if r.embedder == nil {
		return nil, fmt.Errorf("embedding provider is nil")
	}
	if req.Query == "" {
		return &domain.RetrieveResponse{Confidence: domain.ConfidenceLow}, nil
	}

	topK := req.TopK
	if topK <= 0 {
		topK = defaultTopK
	}

	vectors, err := r.embedder.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return &domain.RetrieveResponse{Confidence: domain.ConfidenceLow}, nil
	}

	expr := filter.RetrieveExpr(req.TenantID, req.DocIDs, req.MaxSecretLevel, req.ExcludeSources)
	sp, err := entity.NewIndexHNSWSearchParam(64)
	if err != nil {
		return nil, err
	}

	candidateTopK := topK
	if r.resolver != nil {
		candidateTopK *= generationCandidateMultiplier
		if candidateTopK > maxGenerationCandidateTopK {
			candidateTopK = maxGenerationCandidateTopK
		}
	}
	results, err := r.milvus.Client().Search(
		ctx,
		r.milvus.Collection(),
		nil,
		expr,
		[]string{"content", "metadata"},
		[]entity.Vector{entity.FloatVector(vectors[0])},
		"vector",
		entity.L2,
		candidateTopK,
		sp,
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return &domain.RetrieveResponse{Confidence: domain.ConfidenceLow}, nil
	}

	result := results[0]
	docs := make([]domain.RetrievedDocument, 0, result.ResultCount)
	for idx := 0; idx < result.ResultCount; idx++ {
		rawScore := float64(result.Scores[idx])
		score := normalizeL2Score(rawScore)
		// MinScore historically filtered on L2 distance (lower=closer). With
		// normalized scores we drop hits whose similarity falls below MinScore.
		if req.MinScore > 0 && score < req.MinScore {
			continue
		}

		content := ""
		if col := result.Fields.GetColumn("content"); col != nil {
			if values, ok := col.(*entity.ColumnVarChar); ok && idx < values.Len() {
				content, _ = values.ValueByIdx(idx)
			}
		}

		meta := map[string]any{}
		if col := result.Fields.GetColumn("metadata"); col != nil {
			if values, ok := col.(*entity.ColumnJSONBytes); ok && idx < values.Len() {
				if raw, err := values.ValueByIdx(idx); err == nil {
					_ = json.Unmarshal(raw, &meta)
				}
			}
		}

		chunkID := ""
		if result.IDs != nil && idx < result.IDs.Len() {
			switch idCol := result.IDs.(type) {
			case *entity.ColumnVarChar:
				chunkID, _ = idCol.ValueByIdx(idx)
			}
		}

		layer := classifyLayer(meta)
		doc := domain.RetrievedDocument{
			ChunkID:  chunkID,
			DocID:    stringValue(meta, "doc_id"),
			Content:  content,
			Source:   stringValue(meta, "_source"),
			Score:    applyLayerWeight(score, layer),
			RawScore: rawScore,
			Layer:    layer,
			Version:  stringValue(meta, "version"),
			Service:  stringValue(meta, "service"),
			Metadata: meta,
		}
		docs = append(docs, doc)
	}

	if r.resolver != nil {
		docIDs := uniqueDocumentIDs(docs)
		states, err := r.resolver.ResolveActiveGenerations(ctx, req.TenantID, docIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve active index generations: %w", err)
		}
		docs = filterActiveGenerationDocuments(docs, states)
		if len(docs) > topK {
			docs = docs[:topK]
		}
	}

	topScore := topSimilarity(docs)
	return &domain.RetrieveResponse{
		Documents:  docs,
		TopScore:   topScore,
		Confidence: confidenceFromScore(topScore),
	}, nil
}

func uniqueDocumentIDs(docs []domain.RetrievedDocument) []string {
	seen := make(map[string]struct{}, len(docs))
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		if doc.DocID == "" {
			continue
		}
		if _, ok := seen[doc.DocID]; ok {
			continue
		}
		seen[doc.DocID] = struct{}{}
		ids = append(ids, doc.DocID)
	}
	return ids
}

func filterActiveGenerationDocuments(docs []domain.RetrievedDocument, states map[string]domain.DocumentIndexGeneration) []domain.RetrievedDocument {
	filtered := make([]domain.RetrievedDocument, 0, len(docs))
	for _, doc := range docs {
		if doc.DocID == "" {
			continue
		}
		generation, hasGeneration := metadataGeneration(doc.Metadata)
		state, hasState := states[doc.DocID]
		if !hasState {
			// The resolver returns every active document (including legacy docs
			// without index state). Absence therefore means unknown, deleted, or
			// cross-tenant and must be rejected even if metadata looks legacy.
			continue
		}
		if state.ActiveGeneration == 0 && state.LegacyAllowed && (!hasGeneration || generation == 0) {
			filtered = append(filtered, doc)
			continue
		}
		if hasGeneration && generation == state.ActiveGeneration {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

func metadataGeneration(meta map[string]any) (uint64, bool) {
	if meta == nil {
		return 0, false
	}
	value, ok := meta["generation"]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed != math.Trunc(typed) || typed > math.MaxUint64 {
			return 0, false
		}
		return uint64(typed), true
	case uint64:
		return typed, true
	case int:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	default:
		return 0, false
	}
}

// normalizeL2Score maps an L2 distance to a similarity score in [0,1].
// distance 0 → 1.0, distance >= l2RefDistance → 0.0.
func normalizeL2Score(distance float64) float64 {
	if distance <= 0 {
		return 1.0
	}
	score := 1.0 - distance/l2RefDistance
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// classifyLayer picks the knowledge tier from chunk metadata. Phase 1 keeps a
// single collection, so the layer is derived from the "_layer" / "layer" field
// written by the indexer, defaulting to static for uploaded documents.
func classifyLayer(meta map[string]any) domain.KnowledgeLayer {
	if meta == nil {
		return domain.KnowledgeLayerStatic
	}
	if v := stringValue(meta, "_layer"); v != "" {
		return domain.KnowledgeLayer(v)
	}
	if v := stringValue(meta, "layer"); v != "" {
		return domain.KnowledgeLayer(v)
	}
	return domain.KnowledgeLayerStatic
}

// applyLayerWeight downweights lower-trust tiers so that a strong temp_knowledge
// hit cannot on its own promote a low-confidence query to high-confidence path.
func applyLayerWeight(score float64, layer domain.KnowledgeLayer) float64 {
	switch layer {
	case domain.KnowledgeLayerStatic:
		return score // weight 1.0
	case domain.KnowledgeLayerFaultCase:
		return score * 0.85
	case domain.KnowledgeLayerTemp:
		return score * 0.6
	default:
		return score
	}
}

// topSimilarity returns the highest normalized score across hits, or 0 if empty.
func topSimilarity(docs []domain.RetrievedDocument) float64 {
	top := 0.0
	for _, d := range docs {
		if d.Score > top {
			top = d.Score
		}
	}
	return top
}

// confidenceFromScore maps a normalized top score to a coarse confidence label
// per §6.1 of the refactor design doc.
func confidenceFromScore(topScore float64) domain.ConfidenceLevel {
	// NaN / Inf guard.
	if math.IsNaN(topScore) || math.IsInf(topScore, 0) {
		return domain.ConfidenceLow
	}
	switch {
	case topScore >= highConfThreshold:
		return domain.ConfidenceHigh
	case topScore >= midConfThreshold:
		return domain.ConfidenceMid
	default:
		return domain.ConfidenceLow
	}
}

func stringValue(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}
