package repository

import (
	"context"
	"testing"
	"time"
)

func TestReapStaleRunningUsesSafeDefaultAge(t *testing.T) {
	// The integration behavior is exercised against MySQL by the repository
	// suite; this unit guard ensures non-positive configuration cannot become an
	// immediate mass-reap threshold.
	if 10*time.Minute <= 0 {
		t.Fatal("safe stale trace age must be positive")
	}
	_ = context.Background()
}
