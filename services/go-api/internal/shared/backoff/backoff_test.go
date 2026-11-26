package backoff_test

import (
	"altune/go-api/internal/shared/backoff"
	"testing"
	"time"
)

var policy = backoff.Policy{Base: 100 * time.Millisecond, Cap: time.Second}

func TestPolicy_CeilingDoublesFromTheBaseAndStopsAtTheCap(t *testing.T) {
	want := []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond,
		800 * time.Millisecond, time.Second, time.Second,
	}
	for i, w := range want {
		if got := policy.Ceiling(i + 1); got != w {
			t.Errorf("Ceiling(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestPolicy_CeilingTreatsAnAttemptBelowOneAsTheFirst(t *testing.T) {
	for _, attempt := range []int{0, -3} {
		if got := policy.Ceiling(attempt); got != 100*time.Millisecond {
			t.Errorf("Ceiling(%d) = %v, want the base 100ms", attempt, got)
		}
	}
}

func TestPolicy_CeilingSurvivesAnAttemptCountThatWouldOverflowAShift(t *testing.T) {
	if got := policy.Ceiling(10_000); got != time.Second {
		t.Errorf("Ceiling(10000) = %v, want the cap 1s", got)
	}
}

func TestPolicy_DelayAtSpansHalfTheCeilingToTheCeiling(t *testing.T) {
	cases := map[string]struct {
		unit float64
		want time.Duration
	}{
		"lowest draw":  {0, 200 * time.Millisecond},
		"middle draw":  {0.5, 300 * time.Millisecond},
		"highest draw": {0.999999, 400*time.Millisecond - 1},
	}
	for name, tc := range cases {
		got := policy.DelayAt(3, tc.unit)
		if got < tc.want-time.Microsecond || got > tc.want+time.Microsecond {
			t.Errorf("%s: DelayAt(3, %v) = %v, want %v", name, tc.unit, got, tc.want)
		}
	}
}

func TestPolicy_DelayStaysWithinEqualJitterBounds(t *testing.T) {
	for attempt := 1; attempt <= 8; attempt++ {
		ceiling := policy.Ceiling(attempt)
		for range 200 {
			if got := policy.Delay(attempt); got < ceiling/2 || got > ceiling {
				t.Fatalf("Delay(%d) = %v, want within [%v, %v]", attempt, got, ceiling/2, ceiling)
			}
		}
	}
}
