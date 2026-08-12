package knowledge

import (
	"context"
	"fmt"

	ragindexer "wisesentinel-platform/internal/rag/indexer"

	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
)

// EinoMilvusIndexer adapts the RAG MilvusIndexer to Eino indexer.Indexer.
type EinoMilvusIndexer struct {
	indexer *ragindexer.MilvusIndexer
}

func NewEinoMilvusIndexer(idx *ragindexer.MilvusIndexer) *EinoMilvusIndexer {
	return &EinoMilvusIndexer{indexer: idx}
}

// Store embeds and persists documents into Milvus.
func (i *EinoMilvusIndexer) Store(ctx context.Context, docs []*schema.Document, _ ...indexer.Option) ([]string, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	inputs := make([]ragindexer.ChunkInput, len(docs))
	ids := make([]string, len(docs))
	task := IndexTaskFrom(ctx)
	if task == nil || task.Generation == 0 || task.TaskID == "" {
		return nil, fmt.Errorf("index task generation and task ID are required")
	}
	for idx, doc := range docs {
		chunkIndex := intValue(doc.MetaData, "chunk_index")
		chunkID := ragindexer.DeterministicChunkID(task.TenantID, task.DocID, task.Generation, chunkIndex, doc.Content)
		ids[idx] = chunkID

		inputs[idx] = ragindexer.ChunkInput{
			ChunkID:     chunkID,
			Content:     doc.Content,
			ChunkIndex:  chunkIndex,
			Title:       stringValue(doc.MetaData, "title"),
			TenantID:    stringValue(doc.MetaData, "tenant_id"),
			DocID:       stringValue(doc.MetaData, "doc_id"),
			Source:      stringValue(doc.MetaData, "_source"),
			Visibility:  stringValue(doc.MetaData, "visibility"),
			SecretLevel: intValue(doc.MetaData, "secret_level"),
			Layer:       stringValue(doc.MetaData, "_layer"),
			Version:     stringValue(doc.MetaData, "version"),
			Service:     stringValue(doc.MetaData, "service"),
			Generation:  task.Generation,
			IndexTaskID: task.TaskID,
		}
	}

	count, err := i.indexer.IndexChunks(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if count != len(ids) {
		return nil, fmt.Errorf("indexed %d chunks, expected %d", count, len(ids))
	}
	return ids, nil
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

func intValue(meta map[string]any, key string) int {
	if meta == nil {
		return 0
	}
	v, ok := meta[key]
	if !ok {
		return 0
	}
	switch typed := v.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
