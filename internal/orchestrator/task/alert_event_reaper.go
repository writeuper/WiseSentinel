package task

import (
	"context"
	"time"

	"wisesentinel-platform/internal/observability"
	"wisesentinel-platform/internal/repository"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	alertEventReapAge      = 5 * time.Minute
	alertEventReapInterval = 1 * time.Minute
)

// AlertEventReaper continuously removes Webhook reservations that survived a
// process crash before the corresponding Ops task was persisted.
type AlertEventReaper struct {
	repo     *repository.AlertEventRepo
	interval time.Duration
	age      time.Duration
	health   workerHealth
}

func NewAlertEventReaper(repo *repository.AlertEventRepo) *AlertEventReaper {
	return &AlertEventReaper{repo: repo, interval: alertEventReapInterval, age: alertEventReapAge}
}

func (r *AlertEventReaper) Start(ctx context.Context) {
	if r == nil || r.repo == nil {
		return
	}
	r.health.markStarted()
	go func() {
		defer r.health.markStopped()
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		r.reap(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.reap(ctx)
			}
		}
	}()
}

func (r *AlertEventReaper) reap(ctx context.Context) {
	r.health.markTick()
	reapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	count, err := r.repo.ReapOrphanReservations(reapCtx, r.age)
	if err != nil {
		g.Log().Warning(reapCtx, "AlertEventReaper failed:", err)
		return
	}
	if count > 0 {
		observability.ObserveAlertEventOrphanReaped(count)
		g.Log().Infof(reapCtx, "AlertEventReaper removed orphan reservations: %d", count)
	}
}

func (r *AlertEventReaper) Ready() bool { return r.health.ready(2 * r.interval) }
