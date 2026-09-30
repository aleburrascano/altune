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
	s := NewAudioURLService(nil, nil)
	if s.ttl != audioURLTTL {
		t.Fatalf("ttl = %v, want default %v", s.ttl, audioURLTTL)
	}
}

func TestApplyOptions_ANilClockKeepsTheWallClock(t *testing.T) {
	clocks := map[string]func() time.Time{
		"audio url": NewAudioURLService(nil, nil, WithAudioURLClock(nil)).now,
		"add track": NewAddTrackService(nil, WithAddTrackClock(nil)).now,
	}

	before := time.Now()
	for name, now := range clocks {
		if got := now(); got.Before(before) {
			t.Errorf("%s clock = %v, want the wall clock at or after %v", name, got, before)
		}
	}
}

func TestApplyOptions_ConstructorOptionOverridesDefault(t *testing.T) {
	s := NewAudioURLService(nil, nil, func(s *AudioURLService) { s.ttl = time.Second })
	if s.ttl != time.Second {
		t.Fatalf("ttl = %v, want 1s", s.ttl)
	}
}
