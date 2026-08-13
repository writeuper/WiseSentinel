package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"wisesentinel-platform/internal/pkg/redact"

	"github.com/gogf/gf/v2/frame/g"
)

// AlertEvent is a persisted Alertmanager delivery and its linked task.
type AlertEvent struct {
	TenantID    string
	EventID     string
	IncidentKey string
	Receiver    string
	GroupKey    string
	Status      string
	PayloadJSON string
	TaskID      string
	ReceivedAt  time.Time
	ResolvedAt  *time.Time
}

type AlertEventRepo struct{}

func NewAlertEventRepo() *AlertEventRepo { return &AlertEventRepo{} }

func (r *AlertEventRepo) Get(ctx context.Context, tenantID, eventID string) (*AlertEvent, error) {
	var row AlertEvent
	err := g.DB().Model("ws_alert_event").Ctx(ctx).Where("tenant_id", tenantID).Where("event_id", eventID).Scan(&row)
	if errors.Is(err, sql.ErrNoRows) || row.EventID == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *AlertEventRepo) GetByIncident(ctx context.Context, tenantID, incidentKey string) (*AlertEvent, error) {
	var row AlertEvent
	err := g.DB().Model("ws_alert_event").Ctx(ctx).Where("tenant_id", tenantID).Where("incident_key", incidentKey).OrderDesc("received_at").Scan(&row)
	if errors.Is(err, sql.ErrNoRows) || row.EventID == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *AlertEventRepo) Create(ctx context.Context, event *AlertEvent) error {
	_, err := g.DB().Insert(ctx, "ws_alert_event", g.Map{
		"tenant_id": event.TenantID, "event_id": event.EventID, "incident_key": event.IncidentKey,
		"receiver": event.Receiver, "group_key": event.GroupKey, "status": event.Status,
		"payload_json": redact.JSON(event.PayloadJSON), "task_id": event.TaskID, "received_at": event.ReceivedAt,
	})
	return err
}

func (r *AlertEventRepo) UpdateTaskID(ctx context.Context, tenantID, eventID, taskID string) error {
	_, err := g.DB().Model("ws_alert_event").Ctx(ctx).Where("tenant_id", tenantID).Where("event_id", eventID).Data(g.Map{"task_id": taskID}).Update()
	return err
}

// Delete removes a reservation created before Agent task creation failed. The
// tenant and event predicates ensure a failed delivery cannot delete another
// tenant's or another delivery's record.
func (r *AlertEventRepo) Delete(ctx context.Context, tenantID, eventID string) error {
	_, err := g.DB().Model("ws_alert_event").Ctx(ctx).
		Where("tenant_id", tenantID).Where("event_id", eventID).Delete()
	return err
}

// ReapOrphanReservations removes old firing reservations whose bound Ops task
// was never persisted (for example a process crash between the event insert
// and Agent task creation). The age guard prevents deleting a valid task while
// a transaction or worker is still settling.
func (r *AlertEventRepo) ReapOrphanReservations(ctx context.Context, age time.Duration) (int64, error) {
	if age <= 0 {
		age = 5 * time.Minute
	}
	cutoff := time.Now().Add(-age)
	result, err := g.DB().Exec(ctx, `
DELETE FROM ws_alert_event
 WHERE status = 'firing'
   AND task_id <> ''
   AND received_at < ?
   AND NOT EXISTS (
       SELECT 1 FROM ws_ops_task t
        WHERE t.tenant_id = ws_alert_event.tenant_id
          AND t.task_id = ws_alert_event.task_id
   )`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *AlertEventRepo) MarkResolved(ctx context.Context, tenantID, incidentKey string, resolvedAt time.Time) error {
	_, err := g.DB().Model("ws_alert_event").Ctx(ctx).Where("tenant_id", tenantID).Where("incident_key", incidentKey).Where("status", "firing").Data(g.Map{"status": "resolved", "resolved_at": resolvedAt}).Update()
	return err
}

func MarshalAlertPayload(payload any) string {
	data, _ := json.Marshal(payload)
	return string(data)
}
