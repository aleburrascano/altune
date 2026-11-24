package discoverytest

import (
	"altune/go-api/internal/shared/runloop"
	"testing"
)

const testMaxInFlight = 64

func NewTracker(t testing.TB) *runloop.Tracker {
	t.Helper()
	bg, err := runloop.NewTracker(runloop.TrackerLimits{MaxInFlight: testMaxInFlight})
	if err != nil {
		t.Fatalf("background tracker: %v", err)
	}
	t.Cleanup(bg.Wait)
	return bg
}
