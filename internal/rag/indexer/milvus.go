package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"wisesentinel-platform/internal/rag/client"
	"wisesentinel-platform/internal/rag/embedder"
	"wisesentinel-platform/internal/rag/filter"
	"wisesentinel-platform/internal/rag/splitter"

	sdkclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// operationTimeout bounds Milvus SDK retries (including rate-limit retries).
// An index/GC worker can then renew or release its DB lease and use its
// durable retry policy instead of being held forever by an upstream RPC.
const operationTimeout = 15 * time.Second

func operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, operationTimeout)
}

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
	Layer       string // knowledge tier: static / fault_case / temp; empty = static
	Version     string // service version tag for version-aware retrieval
	Service     string // service name tag for service-aware retrieval
	Generation  uint64 // staged index generation; must be non-zero for async tasks
	IndexTaskID string // task that produced this staged chunk
}

// MilvusIndexer writes chunks into Milvus with embeddings.
type MilvusIndexer struct {
	milvus   *client.MilvusClient
	embedder embedder.Embedder
}

func NewMilvusIndexer(mc *client.MilvusClient, emb embedder.Embedder) *MilvusIndexer {
	return &MilvusIndexer{milvus: mc, embedder: emb}
}

// DeleteByDocID removes existing chunks for a document within its tenant.
func (i *MilvusIndexer) DeleteByDocID(ctx context.Context, tenantID, docID string) error {
	return i.delete(ctx, filter.DeleteByDocIDExpr(tenantID, docID))
}

// DeleteBySource removes existing chunks for a source URI within its tenant.
func (i *MilvusIndexer) DeleteBySource(ctx context.Context, tenantID, source string) error {
	return i.delete(ctx, filter.DeleteBySourceExpr(tenantID, source))
}

// DeleteByGeneration removes only one staged generation for one tenant/document.
func (i *MilvusIndexer) DeleteByGeneration(ctx context.Context, tenantID, docID string, generation uint64) error {
	return i.delete(ctx, filter.DeleteByGenerationExpr(tenantID, docID, generation))
}

// DeleteLegacyByDocID removes only vectors without generation metadata. Milvus
// does not reliably treat generation=0 as a missing JSON field, so this first
// performs a tenant-and-document-scoped scan and then deletes exact IDs.
func (i *MilvusIndexer) DeleteLegacyByDocID(ctx context.Context, tenantID, docID string) error {
	if i.milvus == nil {
		return fmt.Errorf("milvus client is nil")
	}
	const pageSize = 512
	legacyIDs := make([]string, 0)
	for offset := 0; ; offset += pageSize {
		operationCtx, cancel := operationContext(ctx)
		result, err := i.milvus.Client().Query(operationCtx, i.milvus.Collection(), nil,
			filter.DeleteByDocIDExpr(tenantID, docID), []string{"id", "metadata"},
			sdkclient.WithLimit(pageSize), sdkclient.WithOffset(int64(offset)))
		cancel()
		if err != nil {
			return err
		}
		if result.Len() == 0 {
			return nil
		}
		idColumn, ok := result.GetColumn("id").(*entity.ColumnVarChar)
		if !ok {
			return fmt.Errorf("Milvus legacy scan returned no varchar id column")
		}
		metadataColumn, ok := result.GetColumn("metadata").(*entity.ColumnJSONBytes)
		if !ok {
			return fmt.Errorf("Milvus legacy scan returned no metadata column")
		}
		for idx := 0; idx < result.Len(); idx++ {
			id, idErr := idColumn.ValueByIdx(idx)
			raw, metadataErr := metadataColumn.ValueByIdx(idx)
			if idErr != nil || metadataErr != nil {
				return fmt.Errorf("read legacy scan row: id=%v metadata=%v", idErr, metadataErr)
			}
			var metadata map[string]any
			if err := json.Unmarshal(raw, &metadata); err != nil {
				return fmt.Errorf("decode legacy scan metadata: %w", err)
			}
			if _, hasGeneration := metadata["generation"]; !hasGeneration {
				legacyIDs = append(legacyIDs, id)
			}
		}
		if result.Len() < pageSize {
			break
		}
	}
	// Delete only after the complete scan. Deleting during offset pagination
	// shifts later result pages and could silently leave legacy vectors behind.
	for start := 0; start < len(legacyIDs); start += pageSize {
		end := start + pageSize
		if end > len(legacyIDs) {
			end = len(legacyIDs)
		}
		if err := i.delete(ctx, filter.DeleteByIDsExpr(legacyIDs[start:end])); err != nil {
			return err
		}
	}
	return nil
}

func (i *MilvusIndexer) delete(ctx context.Context, expr string) error {
	if i.milvus == nil {
		return fmt.Errorf("milvus client is nil")
	}
	deleteCtx, cancelDelete := operationContext(ctx)
	if err := i.milvus.Client().Delete(deleteCtx, i.milvus.Collection(), "", expr); err != nil {
		cancelDelete()
		return fmt.Errorf("delete Milvus vectors: %w", err)
	}
	cancelDelete()
	// A delete acknowledgement alone may remain buffered. Flush makes the
	// accepted mutation durable before the worker records success; read-side
	// authorization is already fail-closed, but this narrows GC observability
	// and makes replay tests deterministic.
	flushCtx, cancelFlush := operationContext(ctx)
	defer cancelFlush()
	if err := i.milvus.Client().Flush(flushCtx, i.milvus.Collection(), false); err != nil {
		return fmt.Errorf("flush Milvus vector deletion: %w", err)
	}
	return nil
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
	// 调用embedding模型，将文本转换为向量
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
		layer := chunk.Layer
		if layer == "" {
			layer = "static"
		}
		meta := map[string]any{
			"_source":      chunk.Source,
			"tenant_id":    chunk.TenantID,
			"doc_id":       chunk.DocID,
			"chunk_index":  chunk.ChunkIndex,
			"visibility":   chunk.Visibility,
			"secret_level": chunk.SecretLevel,
			"_layer":       layer,
		}
		if chunk.Generation > 0 {
			meta["generation"] = chunk.Generation
		}
		if chunk.IndexTaskID != "" {
			meta["index_task_id"] = chunk.IndexTaskID
		}
		if chunk.Version != "" {
			meta["version"] = chunk.Version
		}
		if chunk.Service != "" {
			meta["service"] = chunk.Service
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
	// Upsert is idempotent for a deterministic generation-scoped chunk ID. A
	// reclaimed worker may write the same staged generation, but cannot create
	// duplicate vectors.
	upsertCtx, cancelUpsert := operationContext(ctx)
	if _, err := i.milvus.Client().Upsert(upsertCtx, i.milvus.Collection(), "", idCol, vectorCol, contentCol, metaCol); err != nil {
		cancelUpsert()
		return 0, fmt.Errorf("upsert Milvus vectors: %w", err)
	}
	cancelUpsert()
	// 刷新数据到Milvus集合
	flushCtx, cancelFlush := operationContext(ctx)
	defer cancelFlush()
	if err := i.milvus.Client().Flush(flushCtx, i.milvus.Collection(), false); err != nil {
		return 0, fmt.Errorf("flush Milvus indexed vectors: %w", err)
	}
	return len(chunks), nil
}

// DeterministicChunkID returns an idempotency key for one staged vector. The
// generation and content hash prevent a newer revision from overwriting an
// older active generation while allowing a retry of the same attempt to upsert.
func DeterministicChunkID(tenantID, docID string, generation uint64, chunkIndex int, content string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", tenantID, docID, generation, chunkIndex, content)))
	return fmt.Sprintf("g_%x", sum[:])
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
