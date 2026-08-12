package embedder

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

const hashDimensions = 2048

// HashEmbedder produces deterministic lexical feature-hash vectors for local
// development and tests. It is deliberately not presented as a semantic model,
// but unlike hashing the complete string it preserves token overlap so local
// retrieval tests can detect ranking regressions.
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
	vec := make([]float32, dim)
	for _, token := range lexicalTokens(text) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		bucket := int(h.Sum64() % uint64(dim))
		// Signed feature hashing limits the impact of collisions without carrying
		// a vocabulary in the process or writing query text to observability.
		if h.Sum64()&1 == 0 {
			vec[bucket]++
		} else {
			vec[bucket]--
		}
	}
	return normalize(vec)
}

func lexicalTokens(text string) []string {
	var tokens []string
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		value := strings.ToLower(string(word))
		tokens = append(tokens, value)
		// CJK text typically has no whitespace. Add adjacent-character features
		// so a query and document can overlap on meaningful terms instead of
		// requiring an identical full sentence.
		if len(word) > 1 && containsNonASCII(word) {
			for i := 0; i+1 < len(word); i++ {
				tokens = append(tokens, string(word[i:i+2]))
			}
		}
		word = word[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			word = append(word, r)
			continue
		}
		flush()
	}
	flush()
	if len(tokens) == 0 {
		return []string{"_empty"}
	}
	return tokens
}

func containsNonASCII(value []rune) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
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
