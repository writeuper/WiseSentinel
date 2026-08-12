package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"wisesentinel-platform/internal/domain"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// DocumentIndexState is the durable authority for a document's staged and
// published vector generations. Generation 0 represents legacy vectors.
type DocumentIndexState struct {
	TenantID          string
	DocID             string
	NextGeneration    uint64
	DesiredGeneration uint64
	ActiveGeneration  uint64
	ActiveTaskID      string
	LegacyAllowed     bool
	PublishedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// DocumentIndexStateRepo manages generation allocation and publication.
type DocumentIndexStateRepo struct{}

func NewDocumentIndexStateRepo() *DocumentIndexStateRepo {
	return &DocumentIndexStateRepo{}
}

// AllocateAndCreateTask atomically assigns the next generation and creates its
// pending task. A newer submission advances desired_generation, so an older
// running task may stage vectors but can never publish them afterwards.
func (r *DocumentIndexStateRepo) AllocateAndCreateTask(ctx context.Context, task *IndexTaskRecord) error {
	if task == nil || task.TenantID == "" || task.DocID == "" || task.TaskID == "" {
		return fmt.Errorf("tenant_id, doc_id and task_id are required")
	}
	if task.Generation != 0 {
		return fmt.Errorf("generation must not be preset")
	}
	if task.Status == "" {
		task.Status = string(domain.IndexTaskPending)
	}
	var generation uint64
	err := g.DB().Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		if _, err := tx.Exec(
			`INSERT IGNORE INTO ws_document_index_state (tenant_id, doc_id) VALUES (?, ?)`,
			task.TenantID, task.DocID,
		); err != nil {
			return err
		}
		var state DocumentIndexState
		if err := tx.GetStruct(&state,
			`SELECT tenant_id, doc_id, next_generation, desired_generation, active_generation, active_task_id, legacy_allowed, published_at, created_at, updated_at
			 FROM ws_document_index_state WHERE tenant_id = ? AND doc_id = ? FOR UPDATE`,
			task.TenantID, task.DocID,
		); err != nil {
			return err
		}
		if state.NextGeneration == math.MaxUint64 {
			return fmt.Errorf("document index generation overflow")
		}
		generation = state.NextGeneration + 1
		result, err := tx.Model("ws_document_index_state").Ctx(txCtx).
			Where("tenant_id", task.TenantID).Where("doc_id", task.DocID).
			Data(g.Map{
				"next_generation":    generation,
				"desired_generation": generation,
				"active_task_id":     task.TaskID,
			}).Update()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("document index state disappeared during allocation")
		}
		_, err = tx.Insert("ws_index_task", g.Map{
			"tenant_id":    task.TenantID,
			"task_id":      task.TaskID,
			"doc_id":       task.DocID,
			"source_uri":   task.SourceURI,
			"visibility":   task.Visibility,
			"secret_level": task.SecretLevel,
			"layer":        string(task.Layer),
			"version":      task.Version,
			"service":      task.Service,
			"status":       task.Status,
			"generation":   generation,
		})
		return err
	})
	if err != nil {
		return err
	}
	task.Generation = generation
	return nil
}

// PublishIfOwned atomically switches active_generation and marks a task
// successful only when the current execution lease, desired generation and
// document lifecycle all still permit publication. A false result is an
// expected fencing outcome (stale token, supersession, deletion), not an error.
func (r *DocumentIndexStateRepo) PublishIfOwned(ctx context.Context, tenantID, taskID, executionToken string, chunkCount int) (bool, error) {
	if tenantID == "" || taskID == "" || executionToken == "" {
		return false, fmt.Errorf("tenant_id, task_id and execution_token are required")
	}
	published := false
	err := g.DB().Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		var task IndexTaskRecord
		if err := tx.GetStruct(&task,
			`SELECT tenant_id, task_id, doc_id, status, execution_token, lease_expires_at, generation
			 FROM ws_index_task WHERE tenant_id = ? AND task_id = ? FOR UPDATE`, tenantID, taskID,
		); err != nil {
			return err
		}
		if task.TaskID == "" || task.Status != string(domain.IndexTaskRunning) || task.ExecutionToken != executionToken || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(time.Now()) || task.Generation == 0 {
			return nil
		}
		var document struct {
			Status string `json:"status"`
		}
		if err := tx.GetStruct(&document,
			`SELECT status FROM ws_document WHERE tenant_id = ? AND doc_id = ? FOR UPDATE`, tenantID, task.DocID,
		); err != nil {
			return err
		}
		if document.Status != "active" {
			return nil
		}
		var state DocumentIndexState
		if err := tx.GetStruct(&state,
			`SELECT tenant_id, doc_id, desired_generation, active_generation, legacy_allowed FROM ws_document_index_state
			 WHERE tenant_id = ? AND doc_id = ? FOR UPDATE`, tenantID, task.DocID,
		); err != nil {
			return err
		}
		if state.DocID == "" || state.DesiredGeneration != task.Generation {
			return nil
		}
		if state.ActiveGeneration > 0 && state.ActiveGeneration != task.Generation {
			if err := enqueueVectorGCTx(txCtx, tx, tenantID, task.DocID, vectorGCTargetGeneration, state.ActiveGeneration, "generation_replaced"); err != nil {
				return err
			}
		}
		if state.LegacyAllowed {
			if err := enqueueVectorGCTx(txCtx, tx, tenantID, task.DocID, vectorGCTargetLegacy, 0, "legacy_superseded"); err != nil {
				return err
			}
		}
		if _, err := tx.Model("ws_document_index_state").Ctx(txCtx).
			Where("tenant_id", tenantID).Where("doc_id", task.DocID).Where("desired_generation", task.Generation).
			Data(g.Map{
				"active_generation": task.Generation,
				"active_task_id":    taskID,
				"legacy_allowed":    false,
				"published_at":      time.Now(),
			}).Update(); err != nil {
			return err
		}
		result, err := tx.Model("ws_index_task").Ctx(txCtx).
			Where("tenant_id", tenantID).Where("task_id", taskID).
			Where("status", string(domain.IndexTaskRunning)).Where("execution_token", executionToken).
			Data(g.Map{
				"status":           string(domain.IndexTaskSuccess),
				"chunk_count":      chunkCount,
				"error_msg":        "",
				"finished_at":      time.Now(),
				"execution_token":  "",
				"lease_expires_at": nil,
			}).Update()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		published = rows == 1
		return nil
	})
	return published, err
}

func (r *DocumentIndexStateRepo) Get(ctx context.Context, tenantID, docID string) (*DocumentIndexState, error) {
	var row DocumentIndexState
	err := g.DB().GetScan(ctx, &row,
		`SELECT tenant_id, doc_id, next_generation, desired_generation, active_generation, active_task_id, legacy_allowed, published_at, created_at, updated_at
		 FROM ws_document_index_state WHERE tenant_id = ? AND doc_id = ?`, tenantID, docID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.DocID == "" {
		return nil, nil
	}
	return &row, nil
}

// CanDeleteGeneration returns false when the target is the document's active
// generation. GC must re-check this immediately before any external delete.
func (r *DocumentIndexStateRepo) CanDeleteGeneration(ctx context.Context, tenantID, docID string, generation uint64) (bool, error) {
	state, err := r.Get(ctx, tenantID, docID)
	if err != nil {
		return false, err
	}
	return state == nil || state.ActiveGeneration != generation, nil
}

// CanDeleteLegacy protects still-readable legacy vectors. A legacy task is
// safe only once the current state no longer permits legacy retrieval.
func (r *DocumentIndexStateRepo) CanDeleteLegacy(ctx context.Context, tenantID, docID string) (bool, error) {
	state, err := r.Get(ctx, tenantID, docID)
	if err != nil {
		return false, err
	}
	return state == nil || !state.LegacyAllowed, nil
}

// CanDeleteDocumentAll protects against an accidental document_all outbox row
// deleting vectors for an active document. A missing document is safe because
// no current application record can authorize retrieval for it.
func (r *DocumentIndexStateRepo) CanDeleteDocumentAll(ctx context.Context, tenantID, docID string) (bool, error) {
	var row struct {
		Status string `json:"status"`
	}
	err := g.DB().Ctx(ctx).Model("ws_document").Fields("status").
		Where("tenant_id", tenantID).Where("doc_id", docID).Scan(&row)
	if err != nil {
		return false, err
	}
	return row.Status != "active", nil
}

// ResolveActiveGenerations returns read-side state only for active documents.
// A returned row without index state is an active legacy document. An absent
// row is not authorized for retrieval (unknown, cross-tenant, or deleted).
func (r *DocumentIndexStateRepo) ResolveActiveGenerations(ctx context.Context, tenantID string, docIDs []string) (map[string]domain.DocumentIndexGeneration, error) {
	resolved := make(map[string]domain.DocumentIndexGeneration)
	if tenantID == "" || len(docIDs) == 0 {
		return resolved, nil
	}
	var rows []struct {
		DocID            string `json:"doc_id"`
		ActiveGeneration uint64 `json:"active_generation"`
		LegacyAllowed    bool   `json:"legacy_allowed"`
	}
	err := g.DB().Ctx(ctx).
		Model("ws_document d").
		LeftJoin("ws_document_index_state s", "s.tenant_id = d.tenant_id AND s.doc_id = d.doc_id").
		Fields("d.doc_id", "COALESCE(s.active_generation, 0) AS active_generation", "COALESCE(s.legacy_allowed, 1) AS legacy_allowed").
		Where("d.tenant_id", tenantID).Where("d.status", "active").WhereIn("d.doc_id", docIDs).Scan(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		resolved[row.DocID] = domain.DocumentIndexGeneration{
			ActiveGeneration: row.ActiveGeneration,
			LegacyAllowed:    row.LegacyAllowed,
		}
	}
	return resolved, nil
}
