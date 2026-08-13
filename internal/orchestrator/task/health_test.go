package task

import (
	"testing"
	"time"
)

func TestWorkerHealthRequiresRecentTickAndNotStopped(t *testing.T) {
	var h workerHealth
	if h.ready(time.Second) {
		t.Fatal("unstarted worker reported ready")
	}
	h.markStarted()
	if h.ready(time.Second) {
		t.Fatal("worker without a tick reported ready")
	}
	h.markTick()
	if !h.ready(time.Second) {
		t.Fatal("worker with a recent tick reported not ready")
	}
	h.markStopped()
	if h.ready(time.Second) {
		t.Fatal("stopped worker reported ready")
	}
}

func TestWorkerHealthExpiresOldTick(t *testing.T) {
	var h workerHealth
	h.markStarted()
	h.lastTick.Store(time.Now().Add(-2 * time.Second).UnixNano())
	if h.ready(time.Second) {
		t.Fatal("stale worker tick reported ready")
	}
}
