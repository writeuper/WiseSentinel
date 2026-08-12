package repository

import (
	"testing"
	"time"
)

func TestRetryBackoffIsBoundedAndMonotonic(t *testing.T) {
	cases := []struct {
		retryCount int
		want       time.Duration
	}{
		{-1, 5 * time.Second},
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{6, 5 * time.Minute},
		{99, 5 * time.Minute},
	}
	for _, tc := range cases {
		if got := RetryBackoff(tc.retryCount); got != tc.want {
			t.Errorf("RetryBackoff(%d) = %s, want %s", tc.retryCount, got, tc.want)
		}
	}
}
