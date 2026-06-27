package indexer

import (
	"context"
	"encoding/json"
	"fmt"

	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/filter"
	"wisesentinel-platform/internal/rag/splitter"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// ChunkInput carries data required to index one chunk.
type ChunkInput struct {
	ChunkID     string
	Content     string
	ChunkIndex  int
	Title       string
	TenantID    string
	DocID       string
	Source      string
	Visibility  string
	SecretLevel int
}

// MilvusIndexer writes chunks into Milvus with embeddings.
type MilvusIndexer struct {
	milvus   *client.MilvusClient
	embedder embedder.Embedder
}

func NewMilvusIndexer(mc *client.MilvusClient, emb embedder.Embedder) *MilvusIndexer {
	return &MilvusIndexer{milvus: mc, embedder: emb}
}

// DeleteByDocID removes existing chunks for a document.
func (i *MilvusIndexer) DeleteByDocID(ctx context.Context, docID string) error {
	return i.delete(ctx, filter.DeleteByDocIDExpr(docID))
}

// DeleteBySource removes existing chunks for a source URI.
func (i *MilvusIndexer) DeleteBySource(ctx context.Context, source string) error {
	return i.delete(ctx, filter.DeleteBySourceExpr(source))
}

func (i *MilvusIndexer) delete(ctx context.Context, expr string) error {
	if i.milvus == nil {
		return fmt.Errorf("milvus client is nil")
	}
	return i.milvus.Client().Delete(ctx, i.milvus.Collection(), "", expr)
}

// IndexChunks embeds and inserts chunks into Milvus.
func (i *MilvusIndexer) IndexChunks(ctx context.Context, chunks []ChunkInput) (int, error) {
	if i.milvus == nil {
		return 0, fmt.Errorf("milvus client is nil")
	}
	if len(chunks) == 0 {
		return 0, nil
	}

	texts := make([]string, len(chunks))
	for idx, chunk := range chunks {
		texts[idx] = chunk.Content
	}
	vectors, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	if len(vectors) != len(chunks) {
		return 0, fmt.Errorf("embedding count mismatch")
	}

	ids := make([]string, len(chunks))
	contents := make([]string, len(chunks))
	metaBytes := make([][]byte, len(chunks))
	floatVectors := make([][]float32, len(chunks))

	for idx, chunk := range chunks {
		ids[idx] = chunk.ChunkID
		contents[idx] = chunk.Content
		floatVectors[idx] = vectors[idx]
		meta := map[string]any{
			"_source":      chunk.Source,
			"tenant_id":    chunk.TenantID,
			"doc_id":       chunk.DocID,
			"chunk_index":  chunk.ChunkIndex,
			"visibility":   chunk.Visibility,
			"secret_level": chunk.SecretLevel,
		}
		if chunk.Title != "" {
			meta["title"] = chunk.Title
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			return 0, err
		}
		metaBytes[idx] = raw
	}

	idCol := entity.NewColumnVarChar("id", ids)
	vectorCol := entity.NewColumnFloatVector("vector", i.embedder.Dimensions(), floatVectors)
	contentCol := entity.NewColumnVarChar("content", contents)
	metaCol := entity.NewColumnJSONBytes("metadata", metaBytes)

	if _, err := i.milvus.Client().Insert(ctx, i.milvus.Collection(), "", idCol, vectorCol, contentCol, metaCol); err != nil {
		return 0, err
	}
	if err := i.milvus.Client().Flush(ctx, i.milvus.Collection(), false); err != nil {
		return 0, err
	}
	return len(chunks), nil
}

// BuildChunkInputs converts splitter chunks into indexer inputs.
func BuildChunkInputs(chunks []splitter.Chunk, tenantID, docID, source, visibility string, secretLevel int) []ChunkInput {
	out := make([]ChunkInput, len(chunks))
	for idx, chunk := range chunks {
		out[idx] = ChunkInput{
			ChunkID:     chunk.ID,
			Content:     chunk.Content,
			ChunkIndex:  chunk.Index,
			Title:       chunk.Title,
			TenantID:    tenantID,
			DocID:       docID,
			Source:      source,
			Visibility:  visibility,
			SecretLevel: secretLevel,
		}
	}
	return out
}
