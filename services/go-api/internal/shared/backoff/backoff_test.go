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

func TestPolicy_CeilingGrowsByTheFactorWhenOneIsSet(t *testing.T) {
	quadrupling := backoff.Policy{Base: 30 * time.Second, Cap: 8 * time.Minute, Factor: 4}
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 8 * time.Minute, 8 * time.Minute}
	for i, w := range want {
		if got := quadrupling.Ceiling(i + 1); got != w {
			t.Errorf("Ceiling(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestPolicy_DelayAtWithASpreadIsSymmetricAroundTheCeiling(t *testing.T) {
	spread := backoff.Policy{Base: 250 * time.Millisecond, Cap: time.Second, Spread: 0.2}
	cases := []struct {
		attempt int
		unit    float64
		want    time.Duration
	}{
		{1, 0.25, 225 * time.Millisecond},
		{1, 0.5, 250 * time.Millisecond},
		{1, 0.75, 275 * time.Millisecond},
		{2, 0.5, 500 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := spread.DelayAt(tc.attempt, tc.unit); got != tc.want {
			t.Errorf("DelayAt(%d, %v) = %v, want %v", tc.attempt, tc.unit, got, tc.want)
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

func TestPolicy_CeilingNeverOverflowsPastACapNearTheLargestDuration(t *testing.T) {
	huge := backoff.Policy{Base: time.Hour, Cap: time.Duration(1<<63 - 1)}

	for attempt := 1; attempt <= 100; attempt++ {
		if got := huge.Ceiling(attempt); got < time.Hour || got > huge.Cap {
			t.Fatalf("Ceiling(%d) = %v, want within [1h, %v]", attempt, got, huge.Cap)
		}
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
