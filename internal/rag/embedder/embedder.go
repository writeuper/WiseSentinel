package embedder

import "context"

// Embedder converts text into dense vectors for Milvus storage and retrieval.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}
