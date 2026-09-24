package history

import (
	"altune/overseer/internal/core"
	"testing"
	"time"
)

// TestTailReturnsOnlyTheMostRecentPointsOldestFirst proves the spark path's
// bounded read: LIMIT is pushed into SQL rather than the caller fetching the
// whole window and trimming in Go, and the result comes back oldest-to-newest
// like Query's.
func TestTailReturnsOnlyTheMostRecentPointsOldestFirst(t *testing.T) {
	d, _ := openTemp(t)
	for i := 0; i < 40; i++ {
		d.Record("reliability", "latency_ms", core.Point{At: base.Add(time.Duration(i) * time.Second), Value: float64(i)})
	}

	points, err := d.Tail("reliability", "latency_ms", base.Add(-time.Hour), base.Add(time.Hour), 30)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(points) != 30 {
		t.Fatalf("Tail returned %d points, want 30", len(points))
	}
	if points[0].Value != 10 {
		t.Errorf("points[0].Value = %v, want 10 (the tail of the last 30 of 40)", points[0].Value)
	}
	if points[29].Value != 39 {
		t.Errorf("points[29].Value = %v, want 39 (the most recent point)", points[29].Value)
	}
}

// TestTailRespectsTheWindow proves the LIMIT push-down does not defeat the
// from/to bound: a point outside the window is never returned even if it would
// otherwise fall within the limit.
func TestTailRespectsTheWindow(t *testing.T) {
	d, _ := openTemp(t)
	d.Record("reliability", "latency_ms", core.Point{At: base.Add(-2 * time.Hour), Value: 1})
	d.Record("reliability", "latency_ms", core.Point{At: base, Value: 2})

	points, err := d.Tail("reliability", "latency_ms", base.Add(-time.Hour), base.Add(time.Hour), 30)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("Tail = %+v, want only the in-window point", points)
	}
}
