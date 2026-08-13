package observability

import "testing"

func TestObserveAlertEventOrphanReapedIgnoresNonPositive(t *testing.T) {
	ObserveAlertEventOrphanReaped(0)
	ObserveAlertEventOrphanReaped(-1)
}
