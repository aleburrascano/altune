package memory

import (
	"altune/go-api/internal/shared/background"
)

func NewTracker(t TB) *background.Tracker {
	t.Helper()
	bg := &background.Tracker{}
	t.Cleanup(bg.Wait)
	return bg
}
