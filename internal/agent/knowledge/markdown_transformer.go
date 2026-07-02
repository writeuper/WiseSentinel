package knowledge

import (
	"context"
	"fmt"

	"wisesentinel-platform/internal/rag/splitter"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// MarkdownTransformer splits markdown documents by headers (Eino document.Transformer).
type MarkdownTransformer struct{}

func NewMarkdownTransformer() *MarkdownTransformer {
	return &MarkdownTransformer{}
}

// Transform splits each input document into chunk documents with inherited metadata.
func (t *MarkdownTransformer) Transform(_ context.Context, src []*schema.Document, _ ...document.TransformerOption) ([]*schema.Document, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("no documents to transform")
	}

	var out []*schema.Document
	for _, doc := range src {
		chunks := splitter.SplitMarkdown(doc.Content)
		if len(chunks) == 0 {
			return nil, fmt.Errorf("no chunks produced from document")
		}
		for _, chunk := range chunks {
			meta := copyMeta(doc.MetaData)
			meta["chunk_index"] = chunk.Index
			if chunk.Title != "" {
				meta["title"] = chunk.Title
			}
			out = append(out, &schema.Document{
				ID:       chunk.ID,
				Content:  chunk.Content,
				MetaData: meta,
			})
		}
	}
	return out, nil
}

func copyMeta(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src)+2)
	for k, v := range src {
		dst[k] = v
	}
	if _, ok := dst["id"]; !ok {
		dst["id"] = uuid.NewString()
	}
	return dst
}
