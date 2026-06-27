package embedder

import (
	"context"
	"crypto/sha256"
	"math"
)

const hashDimensions = 2048

// HashEmbedder produces deterministic normalized vectors for local dev and tests.
type HashEmbedder struct{}

func NewHashEmbedder() *HashEmbedder {
	return &HashEmbedder{}
}

func (e *HashEmbedder) Dimensions() int {
	return hashDimensions
}

func (e *HashEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = hashVector(text, hashDimensions)
	}
	return out, nil
}

func hashVector(text string, dim int) []float32 {
	sum := sha256.Sum256([]byte(text))
	vec := make([]float32, dim)
	for i := 0; i < dim; i++ {
		vec[i] = float32(sum[i%len(sum)]) / 255.0
	}
	return normalize(vec)
}

func normalize(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return vec
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}
