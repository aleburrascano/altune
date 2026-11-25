package discoverytest

import (
	"altune/go-api/internal/shared/runloop"
	"testing"
)

func NewTracker(t testing.TB) *runloop.Tracker {
	t.Helper()
	bg := &runloop.Tracker{}
	t.Cleanup(bg.Wait)
	return bg
}
