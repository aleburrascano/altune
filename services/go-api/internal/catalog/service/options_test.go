package service

import (
	"testing"
	"time"
)

func TestApplyOptions_AppliesInOrderAndReturnsSamePointer(t *testing.T) {
	type cfg struct{ trail []int }
	c := &cfg{}
	got := applyOptions(c, []func(*cfg){
		func(c *cfg) { c.trail = append(c.trail, 1) },
		func(c *cfg) { c.trail = append(c.trail, 2) },
	})
	if got != c {
		t.Fatalf("applyOptions returned a different pointer")
	}
	if len(c.trail) != 2 || c.trail[0] != 1 || c.trail[1] != 2 {
		t.Fatalf("options applied out of order or missing: %v", c.trail)
	}
}

func TestApplyOptions_NoOptionsKeepsDefaults(t *testing.T) {
	s := NewReconcileStalePendingService(nil)
	if s.grace != DefaultStalePendingGrace {
		t.Fatalf("grace = %v, want default %v", s.grace, DefaultStalePendingGrace)
	}
}

func TestApplyOptions_ANilClockKeepsTheWallClock(t *testing.T) {
	clocks := map[string]func() time.Time{
		"audio url":     NewAudioURLService(nil, nil, WithAudioURLClock(nil)).now,
		"stale pending": NewReconcileStalePendingService(nil, WithStalePendingClock(nil)).now,
		"add track":     NewAddTrackService(nil, WithAddTrackClock(nil)).now,
	}

	before := time.Now()
	for name, now := range clocks {
		if got := now(); got.Before(before) {
			t.Errorf("%s clock = %v, want the wall clock at or after %v", name, got, before)
		}
	}
}

func TestApplyOptions_ConstructorOptionOverridesDefault(t *testing.T) {
	s := NewReconcileStalePendingService(nil, func(s *ReconcileStalePendingService) { s.grace = time.Second })
	if s.grace != time.Second {
		t.Fatalf("grace = %v, want 1s", s.grace)
	}
}
