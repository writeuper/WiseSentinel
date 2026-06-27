package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wisesentinel-platform/internal/pkg/configx"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gfile"
)

// LocalStore saves uploaded documents on local disk.
type LocalStore struct {
	baseDir string
}

func NewLocalStore(ctx context.Context) *LocalStore {
	base := configx.String(ctx, "storage.local.base_dir", "FILE_DIR")
	if base == "" {
		base = g.Cfg().MustGet(ctx, "storage.local.base_dir", "/data/wisesentinel/docs").String()
	}
	return &LocalStore{baseDir: base}
}

// Save writes file bytes to {base}/{tenant}/{docID}/{filename}.
func (s *LocalStore) Save(tenantID, docID, filename string, data []byte) (string, error) {
	if err := validateFilename(filename); err != nil {
		return "", err
	}
	dir := filepath.Join(s.baseDir, tenantID, docID)
	if err := gfile.Mkdir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Read loads a document from disk.
func (s *LocalStore) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func validateFilename(name string) error {
	name = filepath.Base(name)
	if name == "" || name == "." || strings.Contains(name, "..") {
		return fmt.Errorf("invalid filename")
	}
	return nil
}
