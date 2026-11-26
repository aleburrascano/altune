package backoff_test

import (
	"altune/go-api/internal/shared/backoff"
	"errors"
	"math"
	"testing"
	"time"
)

var policy = backoff.MustCapped(100*time.Millisecond, time.Second)

func TestCapped_RefusesAnythingThatCannotBeAWait(t *testing.T) {
	cases := map[string]struct {
		build func() (backoff.Policy, error)
		want  error
	}{
		"zero cap":         {func() (backoff.Policy, error) { return backoff.Capped(time.Second, 0) }, backoff.ErrCapNotPositive},
		"negative cap":     {func() (backoff.Policy, error) { return backoff.Capped(time.Second, -time.Second) }, backoff.ErrCapNotPositive},
		"cap below base":   {func() (backoff.Policy, error) { return backoff.Capped(time.Minute, time.Second) }, backoff.ErrCapBelowBase},
		"zero base":        {func() (backoff.Policy, error) { return backoff.Capped(0, time.Second) }, backoff.ErrBaseNotPositive},
		"negative base":    {func() (backoff.Policy, error) { return backoff.Uncapped(-time.Second) }, backoff.ErrBaseNotPositive},
		"factor of one":    {func() (backoff.Policy, error) { return backoff.Uncapped(time.Second, backoff.WithFactor(1)) }, backoff.ErrFactorBelowTwo},
		"negative spread":  {func() (backoff.Policy, error) { return backoff.Uncapped(time.Second, backoff.WithSpread(-0.1)) }, backoff.ErrSpreadOutside},
		"spread above one": {func() (backoff.Policy, error) { return backoff.Uncapped(time.Second, backoff.WithSpread(1.5)) }, backoff.ErrSpreadOutside},
		"spread not a number": {func() (backoff.Policy, error) {
			return backoff.Uncapped(time.Second, backoff.WithSpread(math.NaN()))
		}, backoff.ErrSpreadOutside},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.build(); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMustCapped_PanicsOnABadPolicyAtStartup(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustCapped with a zero cap did not panic")
		}
	}()

	backoff.MustCapped(time.Second, 0)
}

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
	quadrupling := backoff.MustCapped(30*time.Second, 8*time.Minute, backoff.WithFactor(4))
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 8 * time.Minute, 8 * time.Minute}
	for i, w := range want {
		if got := quadrupling.Ceiling(i + 1); got != w {
			t.Errorf("Ceiling(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestPolicy_DelayAtWithASpreadIsSymmetricAroundTheCeiling(t *testing.T) {
	spread := backoff.MustCapped(250*time.Millisecond, time.Second, backoff.WithSpread(0.2))
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

func TestPolicy_ASpreadOnAnUncappedCeilingSaturatesInsteadOfWrappingNegative(t *testing.T) {
	wide := backoff.MustUncapped(time.Second, backoff.WithSpread(0.5))

	for _, unit := range []float64{0, 0.5, 0.99, 1} {
		if got := wide.DelayAt(1000, unit); got <= 0 {
			t.Errorf("DelayAt(1000, %v) = %v, want a positive wait", unit, got)
		}
	}
}

func TestUncapped_GrowsWithoutALimitAndSaturatesInsteadOfOverflowing(t *testing.T) {
	uncapped := backoff.MustUncapped(time.Second)

	if got := uncapped.Ceiling(4); got != 8*time.Second {
		t.Errorf("Ceiling(4) = %v, want 8s", got)
	}
	if got := uncapped.Ceiling(10_000); got <= 0 {
		t.Errorf("Ceiling(10000) = %v, want a positive saturated wait", got)
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
	huge := backoff.MustCapped(time.Hour, time.Duration(math.MaxInt64))

	for attempt := 1; attempt <= 100; attempt++ {
		if got := huge.Ceiling(attempt); got < time.Hour || got > time.Duration(math.MaxInt64) {
			t.Fatalf("Ceiling(%d) = %v, want within [1h, max]", attempt, got)
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
