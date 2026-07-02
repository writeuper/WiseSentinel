package knowledge

import (
	"context"

	"wisesentinel-platform/internal/domain"
)

type indexTaskKey struct{}

// WithIndexTask stores the active index request in context for graph nodes.
func WithIndexTask(ctx context.Context, req *domain.IndexTaskRequest) context.Context {
	return context.WithValue(ctx, indexTaskKey{}, req)
}

// IndexTaskFrom reads the active index request from context.
func IndexTaskFrom(ctx context.Context) *domain.IndexTaskRequest {
	req, _ := ctx.Value(indexTaskKey{}).(*domain.IndexTaskRequest)
	return req
}
