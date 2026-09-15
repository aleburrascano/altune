package core_test

import (
	"altune/overseer/internal/core"
	"testing"
	"time"
)

// Spine invariant: bounded storage, always. Feeding N x capacity must never grow
// the store past its bound.
func TestRingStoreStaysBounded(t *testing.T) {
	const capacity = 8
	s := core.NewRingStore(capacity)

	for i := 0; i < capacity*10; i++ {
		s.Add(core.Signal{At: time.Unix(int64(i), 0), Kind: "tick"})
	}

	if got := s.Len(); got != capacity {
		t.Fatalf("Len = %d, want capped at %d", got, capacity)
	}
	if got := s.Cap(); got != capacity {
		t.Fatalf("Cap = %d, want %d", got, capacity)
	}
	if got := len(s.Snapshot()); got != capacity {
		t.Fatalf("Snapshot len = %d, want %d", got, capacity)
	}
}

// The ring must retain the most recent capacity signals, oldest first.
func TestRingStoreRetainsNewest(t *testing.T) {
	const capacity = 3
	s := core.NewRingStore(capacity)
	for i := 0; i < 5; i++ {
		s.Add(core.Signal{At: time.Unix(int64(i), 0)})
	}

	snap := s.Snapshot()
	want := []int64{2, 3, 4}
	if len(snap) != len(want) {
		t.Fatalf("snapshot len = %d, want %d", len(snap), len(want))
	}
	for i, w := range want {
		if snap[i].At.Unix() != w {
			t.Errorf("snapshot[%d] = %d, want %d", i, snap[i].At.Unix(), w)
		}
	}
}

// A non-positive capacity must be clamped, never unbounded and never panicking.
func TestRingStoreClampsCapacity(t *testing.T) {
	for _, c := range []int{0, -5} {
		s := core.NewRingStore(c)
		s.Add(core.Signal{})
		s.Add(core.Signal{})
		if s.Cap() != 1 || s.Len() != 1 {
			t.Fatalf("capacity %d: Cap=%d Len=%d, want 1/1", c, s.Cap(), s.Len())
		}
	}
}
