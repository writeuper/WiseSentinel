package task

import (
	"sync/atomic"
	"time"
)

// workerHealth is intentionally tiny and lock-free: readiness only needs to
// know whether the scheduler loop is alive and when it last completed a poll.
type workerHealth struct {
	started  atomic.Bool
	stopped  atomic.Bool
	lastTick atomic.Int64
}

func (h *workerHealth) markStarted() { h.started.Store(true); h.stopped.Store(false) }
func (h *workerHealth) markStopped() { h.stopped.Store(true) }
func (h *workerHealth) markTick()    { h.lastTick.Store(time.Now().UnixNano()) }

func (h *workerHealth) ready(maxStale time.Duration) bool {
	if h == nil || !h.started.Load() || h.stopped.Load() {
		return false
	}
	last := h.lastTick.Load()
	return last > 0 && time.Since(time.Unix(0, last)) <= maxStale
}
