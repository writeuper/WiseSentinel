package knowledge

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/storage"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
)

// FileLoader loads document content from local storage (Eino document.Loader).
type FileLoader struct {
	store *storage.LocalStore
}

func NewFileLoader(store *storage.LocalStore) *FileLoader {
	return &FileLoader{store: store}
}

// Load reads the file at src.URI and returns a single schema.Document.
func (l *FileLoader) Load(ctx context.Context, src document.Source, _ ...document.LoaderOption) ([]*schema.Document, error) {
	if src.URI == "" {
		return nil, fmt.Errorf("source uri is required")
	}

	req := IndexTaskFrom(ctx)
	if req == nil {
		return nil, fmt.Errorf("index task missing from context")
	}

	raw, err := l.store.Read(src.URI)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("document file not found: %s", src.URI)
		}
		return nil, err
	}

	secretLevel := req.SecretLevel
	if secretLevel <= 0 {
		secretLevel = 1
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = "tenant"
	}

	meta := map[string]any{
		"_source":      src.URI,
		"tenant_id":    req.TenantID,
		"doc_id":       req.DocID,
		"visibility":   visibility,
		"secret_level": secretLevel,
	}
	if req.Layer != "" {
		meta["_layer"] = string(req.Layer)
	}
	version := documentVersion(req)
	if version != "" {
		meta["version"] = version
	}
	if req.Service != "" {
		meta["service"] = req.Service
	}
	return []*schema.Document{{
		Content:  string(raw),
		MetaData: meta,
	}}, nil
}

func documentVersion(req *domain.IndexTaskRequest) string {
	if req == nil {
		return ""
	}
	if req.Version != "" {
		return req.Version
	}
	if req.Generation > 0 {
		// Ordinary uploads do not carry a business version. Expose the durable
		// generation as a stable, auditable Citation version instead of leaving
		// newly published documents indistinguishable from legacy vectors.
		return "generation-" + strconv.FormatUint(req.Generation, 10)
	}
	return ""
}
