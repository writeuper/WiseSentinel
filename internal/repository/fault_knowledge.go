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

// FaultKnowledge is an auto-generated or reviewed troubleshooting knowledge card.
type FaultKnowledge struct {
	CardID      string     `json:"card_id" orm:"card_id"`
	TenantID    string     `json:"tenant_id" orm:"tenant_id"`
	TaskID      string     `json:"task_id" orm:"task_id"`
	TraceID     string     `json:"trace_id" orm:"trace_id"`
	Title       string     `json:"title" orm:"title"`
	Symptom     string     `json:"symptom" orm:"symptom"`
	Impact      string     `json:"impact" orm:"impact"`
	RootCause   string     `json:"root_cause" orm:"root_cause"`
	Workaround  string     `json:"workaround" orm:"workaround"`
	Remediation string     `json:"remediation" orm:"remediation"`
	Evidence    string     `json:"evidence_json" orm:"evidence_json"`
	Service     string     `json:"service" orm:"service"`
	Version     string     `json:"version" orm:"version"`
	Status      string     `json:"status" orm:"status"`
	Weight      float64    `json:"weight" orm:"weight"`
	DocID       string     `json:"doc_id" orm:"doc_id"`
	HitCount    int        `json:"hit_count" orm:"hit_count"`
	UsefulCount int        `json:"useful_count" orm:"useful_count"`
	BadCount    int        `json:"bad_count" orm:"bad_count"`
	CreatedBy   string     `json:"created_by" orm:"created_by"`
	ReviewedBy  string     `json:"reviewed_by" orm:"reviewed_by"`
	CreatedAt   time.Time  `json:"created_at" orm:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" orm:"updated_at"`
	ReviewedAt  *time.Time `json:"reviewed_at" orm:"reviewed_at"`
}

type FaultKnowledgeRepo struct{}

func NewFaultKnowledgeRepo() *FaultKnowledgeRepo { return &FaultKnowledgeRepo{} }

func stringOrNullJSON(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *FaultKnowledgeRepo) CreateDraft(ctx context.Context, card *FaultKnowledge) error {
	_, err := g.DB().InsertIgnore(ctx, "ws_fault_knowledge", g.Map{
		"tenant_id":     card.TenantID,
		"card_id":       card.CardID,
		"task_id":       card.TaskID,
		"trace_id":      card.TraceID,
		"title":         card.Title,
		"symptom":       card.Symptom,
		"impact":        card.Impact,
		"root_cause":    card.RootCause,
		"workaround":    card.Workaround,
		"remediation":   card.Remediation,
		"evidence_json": stringOrNullJSON(card.Evidence),
		"service":       card.Service,
		"version":       card.Version,
		"status":        card.Status,
		"weight":        card.Weight,
		"created_by":    card.CreatedBy,
	})
	return err
}

func (r *FaultKnowledgeRepo) List(ctx context.Context, tenantID, status string, page, size int) ([]FaultKnowledge, int, error) {
	page, size = normalizePageBounds(page, size)
	model := g.DB().Model("ws_fault_knowledge").Ctx(ctx).Where("tenant_id", tenantID)
	if status != "" {
		model = model.Where("status", status)
	}
	total, err := model.Count()
	if err != nil {
		return nil, 0, err
	}
	var rows []FaultKnowledge
	err = model.OrderDesc("updated_at").Page(page, size).Scan(&rows)
	return rows, total, err
}

func (r *FaultKnowledgeRepo) Get(ctx context.Context, tenantID, cardID string) (*FaultKnowledge, error) {
	var row FaultKnowledge
	err := g.DB().Model("ws_fault_knowledge").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("card_id", cardID).
		Scan(&row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.CardID == "" {
		return nil, nil
	}
	return &row, nil
}

func (r *FaultKnowledgeRepo) Approve(ctx context.Context, tenantID, cardID, reviewerID, docID string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_fault_knowledge").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("card_id", cardID).
		Data(g.Map{
			"status":      "approved",
			"reviewed_by": reviewerID,
			"reviewed_at": now,
			"doc_id":      docID,
		}).Update()
	return err
}

func (r *FaultKnowledgeRepo) Reject(ctx context.Context, tenantID, cardID, reviewerID string) error {
	now := time.Now()
	_, err := g.DB().Model("ws_fault_knowledge").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("card_id", cardID).
		Data(g.Map{"status": "rejected", "reviewed_by": reviewerID, "reviewed_at": now}).Update()
	return err
}

func (r *FaultKnowledgeRepo) Feedback(ctx context.Context, tenantID, cardID string, useful bool) error {
	field := "bad_count"
	weightDelta := -0.1
	if useful {
		field = "useful_count"
		weightDelta = 0.05
	}
	_, err := g.DB().Model("ws_fault_knowledge").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("card_id", cardID).
		Increment(field, 1)
	if err != nil {
		return err
	}
	_, err = g.DB().Model("ws_fault_knowledge").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("card_id", cardID).
		Data(g.Map{"weight": gdb.Raw(fmt.Sprintf("GREATEST(0.1, LEAST(1.5, weight + %.2f))", weightDelta))}).
		Update()
	return err
}
