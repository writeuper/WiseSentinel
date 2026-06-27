package retriever

import (
	"context"
	"encoding/json"
	"fmt"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/filter"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const defaultTopK = 3

// MilvusRetriever performs vector search with tenant filters.
type MilvusRetriever struct {
	milvus   *client.MilvusClient
	embedder embedder.Embedder
}

func NewMilvusRetriever(mc *client.MilvusClient, emb embedder.Embedder) *MilvusRetriever {
	return &MilvusRetriever{milvus: mc, embedder: emb}
}

// Retrieve searches Milvus and maps hits to domain documents.
func (r *MilvusRetriever) Retrieve(ctx context.Context, req *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	if r.milvus == nil {
		return nil, fmt.Errorf("milvus client is nil")
	}
	if req.Query == "" {
		return &domain.RetrieveResponse{}, nil
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
		return &domain.RetrieveResponse{}, nil
	}

	expr := filter.RetrieveExpr(req.TenantID, req.DocIDs)
	sp, err := entity.NewIndexHNSWSearchParam(64)
	if err != nil {
		return nil, err
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
		topK,
		sp,
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return &domain.RetrieveResponse{}, nil
	}

	result := results[0]
	docs := make([]domain.RetrievedDocument, 0, result.ResultCount)
	for idx := 0; idx < result.ResultCount; idx++ {
		score := float64(result.Scores[idx])
		if req.MinScore > 0 && score > req.MinScore {
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

		doc := domain.RetrievedDocument{
			ChunkID:  chunkID,
			DocID:    stringValue(meta, "doc_id"),
			Content:  content,
			Source:   stringValue(meta, "_source"),
			Score:    score,
			Metadata: meta,
		}
		docs = append(docs, doc)
	}

	return &domain.RetrieveResponse{Documents: docs}, nil
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
