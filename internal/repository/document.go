package repository

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Document is a ws_document row.
type Document struct {
	TenantID    string
	DocID       string
	Name        string
	SourceURI   string
	MimeType    string
	Visibility  string
	SecretLevel int
	Status      string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DocumentRepo manages ws_document persistence.
type DocumentRepo struct{}

func NewDocumentRepo() *DocumentRepo {
	return &DocumentRepo{}
}

func (r *DocumentRepo) Create(ctx context.Context, doc *Document) error {
	_, err := g.DB().Insert(ctx, "ws_document", g.Map{
		"tenant_id":    doc.TenantID,
		"doc_id":       doc.DocID,
		"name":         doc.Name,
		"source_uri":   doc.SourceURI,
		"mime_type":    doc.MimeType,
		"visibility":   doc.Visibility,
		"secret_level": doc.SecretLevel,
		"status":       doc.Status,
		"created_by":   doc.CreatedBy,
	})
	return err
}

func (r *DocumentRepo) Get(ctx context.Context, tenantID, docID string) (*Document, error) {
	var row struct {
		TenantID    string    `json:"tenant_id"`
		DocID       string    `json:"doc_id"`
		Name        string    `json:"name"`
		SourceURI   string    `json:"source_uri"`
		MimeType    string    `json:"mime_type"`
		Visibility  string    `json:"visibility"`
		SecretLevel int       `json:"secret_level"`
		Status      string    `json:"status"`
		CreatedBy   string    `json:"created_by"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}
	err := g.DB().Model("ws_document").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("doc_id", docID).
		Scan(&row)
	if err != nil {
		return nil, err
	}
	if row.DocID == "" {
		return nil, nil
	}
	return &Document{
		TenantID:    row.TenantID,
		DocID:       row.DocID,
		Name:        row.Name,
		SourceURI:   row.SourceURI,
		MimeType:    row.MimeType,
		Visibility:  row.Visibility,
		SecretLevel: row.SecretLevel,
		Status:      row.Status,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (r *DocumentRepo) List(ctx context.Context, tenantID, status string, page, size int) ([]Document, int, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	model := g.DB().Model("ws_document").Ctx(ctx).Where("tenant_id", tenantID)
	if status != "" {
		model = model.Where("status", status)
	} else {
		model = model.WhereNot("status", "deleted")
	}

	total, err := model.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []struct {
		DocID      string    `json:"doc_id"`
		Name       string    `json:"name"`
		Status     string    `json:"status"`
		Visibility string    `json:"visibility"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	err = model.Page(page, size).OrderDesc("updated_at").Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	items := make([]Document, len(rows))
	for i, row := range rows {
		items[i] = Document{
			TenantID:   tenantID,
			DocID:      row.DocID,
			Name:       row.Name,
			Status:     row.Status,
			Visibility: row.Visibility,
			UpdatedAt:  row.UpdatedAt,
		}
	}
	return items, total, nil
}

func (r *DocumentRepo) SoftDelete(ctx context.Context, tenantID, docID string) error {
	_, err := g.DB().Model("ws_document").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("doc_id", docID).
		Data(g.Map{"status": "deleted"}).
		Update()
	return err
}
