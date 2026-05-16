package whatttft

import "testing"

// TestRealClockNowReturnsNonZeroTime verifies RealClock reads a usable timestamp.
func TestRealClockNowReturnsNonZeroTime(t *testing.T) {
	if (RealClock{}).Now().IsZero() {
		t.Fatal("real clock returned zero time")
	}
}
