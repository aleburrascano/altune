package searchtest

import (
	"altune/go-api/internal/shared/background"
	"testing"
)

func NewTracker(t testing.TB) *background.Tracker {
	t.Helper()
	bg := &background.Tracker{}
	t.Cleanup(bg.Wait)
	return bg
}
