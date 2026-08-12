package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
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

// RAGInventory is the global logical retrieval inventory. It intentionally
// contains no tenant or document dimensions so it can be exported safely as
// a low-cardinality platform metric.
type RAGInventory struct {
	ActiveDocuments       int
	ActivePublishedChunks int
	ActiveLegacyDocuments int
}

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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
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

// SoftDeleteAndEnqueueVectorGC atomically fences runnable index tasks, makes
// the document lifecycle transition, and records the document_all cleanup
// intent. Keeping all three state changes in one transaction prevents a
// failed delete from leaving an active document whose index work was already
// cancelled. Retrieval fails closed as soon as the transaction commits;
// physical Milvus deletion is retried separately.
func (r *DocumentRepo) SoftDeleteAndEnqueueVectorGC(ctx context.Context, tenantID, docID string) error {
	if tenantID == "" || docID == "" {
		return fmt.Errorf("tenant_id and doc_id are required")
	}
	return g.DB().Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		// Lock task rows before the document row. PublishIfOwned follows the
		// same task-then-document order, avoiding an inverted lock order during
		// delete-versus-publish races. Clearing the token fences an in-flight
		// worker from publishing after this transaction commits.
		if _, err := tx.Model("ws_index_task").Ctx(txCtx).
			Where("tenant_id", tenantID).Where("doc_id", docID).
			WhereIn("status", []string{"pending", "running"}).
			Data(g.Map{
				"status":           "failed",
				"error_msg":        "document deleted",
				"finished_at":      time.Now(),
				"execution_token":  "",
				"lease_expires_at": nil,
			}).Update(); err != nil {
			return err
		}
		result, err := tx.Model("ws_document").Ctx(txCtx).
			Where("tenant_id", tenantID).Where("doc_id", docID).Where("status", "active").
			Data(g.Map{"status": "deleted"}).Update()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("active document not found")
		}
		return enqueueVectorGCTx(txCtx, tx, tenantID, docID, VectorGCTargetDocumentAll, 0, "document_deleted")
	})
}

// ActiveRAGInventory counts only active documents and their current published
// index generation. Historical successful tasks, superseded generations and
// deleted documents are intentionally excluded; physical Milvus rows need a
// separate collection/GC measurement and must not be inferred from this SQL.
func (r *DocumentRepo) ActiveRAGInventory(ctx context.Context) (RAGInventory, error) {
	var inventory RAGInventory
	err := g.DB().GetScan(ctx, &inventory, `
		SELECT
			COUNT(*) AS active_documents,
			COALESCE(SUM(CASE
				WHEN state.active_generation > 0
				 AND task.status = 'success'
				 AND task.generation = state.active_generation
				THEN task.chunk_count ELSE 0 END), 0) AS active_published_chunks,
			COALESCE(SUM(CASE
				WHEN state.doc_id IS NULL OR state.active_generation = 0
				THEN 1 ELSE 0 END), 0) AS active_legacy_documents
		FROM ws_document document
		LEFT JOIN ws_document_index_state state
			ON state.tenant_id = document.tenant_id AND state.doc_id = document.doc_id
		LEFT JOIN ws_index_task task
			ON task.tenant_id = document.tenant_id
			AND task.doc_id = document.doc_id
			AND task.generation = state.active_generation
		WHERE document.status = 'active'
	`)
	return inventory, err
}
