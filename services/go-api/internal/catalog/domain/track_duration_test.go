package domain

import (
	"errors"
	"math"
	"testing"
)

func TestTrackSetDuration_RejectsUnstorableValues(t *testing.T) {
	t.Parallel()
	for name, seconds := range map[string]float64{
		"+Inf":      math.Inf(1),
		"-Inf":      math.Inf(-1),
		"NaN":       math.NaN(),
		"negative":  -1,
		"above cap": MaxDurationSeconds + 1,
		"max float": math.MaxFloat64,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			track := &Track{}

			err := track.SetDuration(seconds)

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("SetDuration(%v) error = %v, want a ValidationError", seconds, err)
			}
			if track.DurationSeconds != nil {
				t.Errorf("DurationSeconds = %v, want unset", *track.DurationSeconds)
			}
		})
	}
}

func TestTrackSetDuration_StoresPositiveAndIgnoresZero(t *testing.T) {
	t.Parallel()
	track := &Track{}

	if err := track.SetDuration(0); err != nil || track.DurationSeconds != nil {
		t.Fatalf("SetDuration(0) = %v, duration %v; want nil error and unset", err, track.DurationSeconds)
	}
	if err := track.SetDuration(MaxDurationSeconds); err != nil {
		t.Fatalf("SetDuration(max) error = %v", err)
	}
	if track.DurationSeconds == nil || *track.DurationSeconds != MaxDurationSeconds {
		t.Errorf("DurationSeconds = %v, want %d", track.DurationSeconds, MaxDurationSeconds)
	}
}

// With every stored duration at most the cap, a playlist total stays finite
// and therefore JSON-encodable.
func TestTotalDurationSeconds_FiniteAtCap(t *testing.T) {
	t.Parallel()
	tracks := make([]*Track, 10000)
	for i := range tracks {
		tracks[i] = &Track{}
		if err := tracks[i].SetDuration(MaxDurationSeconds); err != nil {
			t.Fatal(err)
		}
	}

	if total := TotalDurationSeconds(tracks); math.IsInf(total, 0) || math.IsNaN(total) {
		t.Errorf("TotalDurationSeconds = %v, want finite", total)
	}
}
