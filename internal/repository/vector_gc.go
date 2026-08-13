package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// VectorGCTask is one durable, fenced cleanup attempt.
type VectorGCTask struct {
	TenantID         string
	DocID            string
	TargetKey        string
	TargetKind       string
	TargetGeneration uint64
	Status           string
	AttemptCount     int
	MaxAttempts      int
	ExecutionToken   string
	NextAttemptAt    *time.Time
	LeaseExpiresAt   *time.Time
	LastError        string
}

const (
	VectorGCTargetGeneration  = "generation"
	VectorGCTargetLegacy      = "legacy"
	VectorGCTargetDocumentAll = "document_all"
)

// VectorGCRepo owns the durable vector cleanup outbox. Its database CAS is
// the correctness boundary; callers must not replace it with a cache lock.
type VectorGCRepo struct{}

func NewVectorGCRepo() *VectorGCRepo { return &VectorGCRepo{} }

func (r *VectorGCRepo) ListRunnable(ctx context.Context, limit int, now time.Time) ([]*VectorGCTask, error) {
	if limit <= 0 {
		limit = 10
	}
	var tasks []*VectorGCTask
	err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
		Where("status = ? OR (status = ? AND lease_expires_at < ?) OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))", "pending", "running", now, "retry_wait", now).
		Order("created_at ASC").Limit(limit).Scan(&tasks)
	return tasks, err
}

// CountByStatus returns aggregate queue state for low-cardinality metrics. It
// deliberately does not group by tenant, document, or error content.
func (r *VectorGCRepo) CountByStatus(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Fields("status, COUNT(*) AS count").Group("status").Scan(&rows)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Status] = row.Count
	}
	return counts, nil
}

// CountDeadSince reports newly dead-lettered tasks in a bounded time window.
// It complements the total dead gauge, which intentionally includes historical
// backlog for administrative cleanup but is too noisy for incident alerts.
func (r *VectorGCRepo) CountDeadSince(ctx context.Context, since time.Time) (int, error) {
	count, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
		Where("status", "dead").WhereGTE("updated_at", since).Count()
	return count, err
}

func (r *VectorGCRepo) Get(ctx context.Context, tenantID, docID, targetKey string) (*VectorGCTask, error) {
	var task VectorGCTask
	err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
		Where("tenant_id", tenantID).Where("doc_id", docID).Where("target_key", targetKey).Scan(&task)
	if err != nil {
		return nil, err
	}
	if task.TargetKey == "" {
		return nil, nil
	}
	return &task, nil
}

// List returns one tenant's GC tasks for the admin operations surface.
func (r *VectorGCRepo) List(ctx context.Context, tenantID, status string, page, size int) ([]*VectorGCTask, int, error) {
	page, size = normalizePageBounds(page, size)
	model := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").Where("tenant_id", tenantID)
	if status != "" {
		model = model.Where("status", status)
	}
	total, err := model.Count()
	if err != nil {
		return nil, 0, err
	}
	var tasks []*VectorGCTask
	err = model.OrderDesc("updated_at").Page(page, size).Scan(&tasks)
	return tasks, total, err
}

type vectorGCRedrivePayload struct {
	DocID       string `json:"doc_id"`
	TargetKey   string `json:"target_key"`
	RequestedBy string `json:"requested_by"`
}

const vectorGCRedriveApprovalType = "vector_gc_redrive"

// VectorGCRedriveApprovalType is used by the handler without allowing callers
// to supply arbitrary approval categories.
const VectorGCRedriveApprovalType = vectorGCRedriveApprovalType

// RedriveApprovalTaskID creates a short stable approval reference independent
// of attacker-controlled document/target lengths.
func RedriveApprovalTaskID(docID, targetKey string) string {
	sum := sha256.Sum256([]byte(docID + "\x00" + targetKey))
	return "gc-redrive-" + hex.EncodeToString(sum[:24])
}

// RequestRedriveApproval creates at most one unexpired approval for a dead
// target. The task row is locked first so concurrent administrators receive
// the same pending approval rather than creating a replay storm.
func (r *VectorGCRepo) RequestRedriveApproval(ctx context.Context, tenantID, docID, targetKey, requesterID, approvalID, payloadJSON string, expiresAt time.Time) (string, bool, error) {
	var payload vectorGCRedrivePayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil || payload.DocID != docID || payload.TargetKey != targetKey || payload.RequestedBy != requesterID || requesterID == "" {
		return "", false, fmt.Errorf("invalid vector GC redrive approval payload")
	}
	ref := RedriveApprovalTaskID(docID, targetKey)
	created := false
	resolvedID := ""
	err := g.DB().Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		var task VectorGCTask
		if err := tx.GetStruct(&task, `SELECT tenant_id, doc_id, target_key, status FROM ws_rag_vector_gc_task WHERE tenant_id = ? AND doc_id = ? AND target_key = ? FOR UPDATE`, tenantID, docID, targetKey); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("vector GC target not found")
			}
			return err
		}
		if task.TargetKey == "" || task.Status != "dead" {
			return fmt.Errorf("only dead vector GC tasks can be redriven")
		}
		var pending Approval
		if err := tx.GetStruct(&pending, `SELECT approval_id FROM ws_approval WHERE tenant_id = ? AND task_id = ? AND approval_type = ? AND status = 'pending' AND expired_at > ? ORDER BY created_at DESC LIMIT 1`, tenantID, ref, vectorGCRedriveApprovalType, time.Now()); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if pending.ApprovalID != "" {
			resolvedID = pending.ApprovalID
			return nil
		}
		if _, err := tx.Insert("ws_approval", g.Map{
			"tenant_id": tenantID, "approval_id": approvalID, "task_id": ref, "approval_type": vectorGCRedriveApprovalType,
			"payload_json": payloadJSON, "status": "pending", "expired_at": expiresAt,
		}); err != nil {
			return err
		}
		resolvedID, created = approvalID, true
		return nil
	})
	return resolvedID, created, err
}

// ApproveRedrive atomically records a second administrator's approval and
// requeues the exact dead task. It cannot run a target changed by another
// recovery path, and the requester cannot approve their own action.
func (r *VectorGCRepo) ApproveRedrive(ctx context.Context, tenantID, approvalID, approverID string) (bool, error) {
	approved := false
	err := g.DB().Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		var approval Approval
		if err := tx.GetStruct(&approval, `SELECT tenant_id, approval_id, approval_type, payload_json, status, expired_at FROM ws_approval WHERE tenant_id = ? AND approval_id = ? FOR UPDATE`, tenantID, approvalID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if approval.ApprovalID == "" || approval.ApprovalType != vectorGCRedriveApprovalType || approval.Status != "pending" || !approval.ExpiredAt.After(time.Now()) {
			return nil
		}
		var payload vectorGCRedrivePayload
		if err := json.Unmarshal([]byte(approval.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("invalid vector GC redrive approval payload")
		}
		if payload.DocID == "" || payload.TargetKey == "" || payload.RequestedBy == "" || payload.RequestedBy == approverID {
			return fmt.Errorf("vector GC redrive requires a different approver")
		}
		result, err := tx.Model("ws_rag_vector_gc_task").Ctx(txCtx).
			Where("tenant_id", tenantID).Where("doc_id", payload.DocID).Where("target_key", payload.TargetKey).Where("status", "dead").
			Data(g.Map{"status": "pending", "attempt_count": 0, "next_attempt_at": nil, "lease_expires_at": nil, "execution_token": "", "last_error": "manual redrive approved", "finished_at": nil}).Update()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("dead vector GC target no longer exists")
		}
		result, err = tx.Model("ws_approval").Ctx(txCtx).
			Where("tenant_id", tenantID).Where("approval_id", approvalID).Where("status", "pending").
			Data(g.Map{"status": "approved", "approver_id": approverID, "updated_at": time.Now()}).Update()
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("vector GC redrive approval was concurrently changed")
		}
		approved = true
		return nil
	})
	return approved, err
}

// ClaimVectorGC grants an expiring DB lease. Redis is intentionally not part
// of this correctness boundary.
func ClaimVectorGC(ctx context.Context, tenantID, docID, targetKey, token string, leaseUntil time.Time) (bool, error) {
	return NewVectorGCRepo().Claim(ctx, tenantID, docID, targetKey, token, leaseUntil)
}

// Claim grants an expiring database lease and increments the durable attempt
// counter. A reclaimed lease receives a new token, fencing the stale owner.
func (r *VectorGCRepo) Claim(ctx context.Context, tenantID, docID, targetKey, token string, leaseUntil time.Time) (bool, error) {
	now := time.Now()
	result, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
		Where("tenant_id", tenantID).Where("doc_id", docID).Where("target_key", targetKey).
		Where("status = ? OR (status = ? AND lease_expires_at < ?) OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))", "pending", "running", now, "retry_wait", now).
		Data(g.Map{"status": "running", "execution_token": token, "lease_expires_at": leaseUntil, "next_attempt_at": nil, "attempt_count": gdb.Raw("attempt_count + 1")}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// FinishVectorGCIfOwned records a terminal status only for the current lease.
func FinishVectorGCIfOwned(ctx context.Context, tenantID, docID, targetKey, token, status, lastError string) (bool, error) {
	return NewVectorGCRepo().FinishIfOwned(ctx, tenantID, docID, targetKey, token, status, lastError)
}

// FinishIfOwned writes a terminal outcome only for the worker holding the
// current DB execution token.
func (r *VectorGCRepo) FinishIfOwned(ctx context.Context, tenantID, docID, targetKey, token, status, lastError string) (bool, error) {
	if status != "succeeded" && status != "dead" && status != "skipped" {
		return false, fmt.Errorf("invalid vector GC terminal status %q", status)
	}
	result, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
		Where("tenant_id", tenantID).Where("doc_id", docID).Where("target_key", targetKey).
		Where("status", "running").Where("execution_token", token).
		Data(g.Map{"status": status, "execution_token": "", "lease_expires_at": nil, "last_error": redact.Summary(lastError, 1000), "finished_at": time.Now()}).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// RetryIfOwned releases a failed attempt into retry_wait, or dead-letters it
// after its persisted attempt budget is exhausted. It never changes a task
// that was reclaimed by another worker.
func (r *VectorGCRepo) RetryIfOwned(ctx context.Context, task *VectorGCTask, token string, retryAt time.Time, lastError string) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("vector GC task is required")
	}
	status := "retry_wait"
	finishedAt := any(nil)
	if task.AttemptCount >= task.MaxAttempts {
		status = "dead"
		finishedAt = time.Now()
		retryAt = time.Time{}
	}
	data := g.Map{
		"status": status, "execution_token": "", "lease_expires_at": nil,
		"last_error": redact.Summary(lastError, 1000), "finished_at": finishedAt,
	}
	if status == "retry_wait" {
		data["next_attempt_at"] = retryAt
	} else {
		data["next_attempt_at"] = nil
	}
	result, err := g.DB().Ctx(ctx).Model("ws_rag_vector_gc_task").
		Where("tenant_id", task.TenantID).Where("doc_id", task.DocID).Where("target_key", task.TargetKey).
		Where("status", "running").Where("execution_token", token).Data(data).Update()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

const (
	vectorGCTargetGeneration = VectorGCTargetGeneration
	vectorGCTargetLegacy     = VectorGCTargetLegacy
)

// enqueueVectorGCTx records an idempotent cleanup intent in the same database
// transaction that made a generation obsolete. The worker is introduced in a
// later stage; storing the intent now prevents crash windows from losing it.
func enqueueVectorGCTx(ctx context.Context, tx gdb.TX, tenantID, docID, targetKind string, generation uint64, reason string) error {
	targetKey := ""
	switch targetKind {
	case vectorGCTargetGeneration:
		targetKey = fmt.Sprintf("generation:%d", generation)
	case vectorGCTargetLegacy:
		targetKey = "legacy:v1"
	case VectorGCTargetDocumentAll:
		targetKey = "document:all"
	default:
		return fmt.Errorf("unsupported vector GC target kind %q", targetKind)
	}
	_, err := tx.InsertIgnore("ws_rag_vector_gc_task", g.Map{
		"tenant_id":         tenantID,
		"doc_id":            docID,
		"target_key":        targetKey,
		"target_kind":       targetKind,
		"target_generation": generation,
		"reason":            reason,
		"status":            "pending",
	})
	return err
}
