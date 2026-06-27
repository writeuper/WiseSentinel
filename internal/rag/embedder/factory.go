package embedder

import (
	"context"
	"strings"

	"wisesentinel-platform/internal/pkg/configx"
)

// NewFromConfig returns DashScope embedder when EMBED_API_KEY is set, otherwise HashEmbedder.
func NewFromConfig(ctx context.Context) (Embedder, error) {
	if strings.TrimSpace(configx.String(ctx, "models.profiles.embedding_default.api_key", "EMBED_API_KEY")) != "" {
		return NewDashScopeEmbedder(ctx)
	}
	return NewHashEmbedder(), nil
}
