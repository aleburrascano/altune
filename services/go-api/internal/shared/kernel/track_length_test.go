package kernel_test

import (
	"altune/go-api/internal/shared/kernel"
	"errors"
	"math"
	"testing"
)

func TestTrackLengthOf_AcceptsAnyFiniteLengthFromZeroToSevenDays(t *testing.T) {
	tests := []struct {
		name      string
		seconds   float64
		wantKnown bool
	}{
		{"zero is an unknown length", 0, false},
		{"a fraction of a second", 0.5, true},
		{"a three minute song", 204, true},
		{"exactly seven days", 7 * 24 * 60 * 60, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kernel.TrackLengthOf(tt.seconds)
			if err != nil {
				t.Fatalf("TrackLengthOf(%v) error = %v, want nil", tt.seconds, err)
			}
			if got.Known() != tt.wantKnown {
				t.Errorf("Known() = %v, want %v", got.Known(), tt.wantKnown)
			}
			if got.Seconds() != tt.seconds {
				t.Errorf("Seconds() = %v, want the %v it was made from", got.Seconds(), tt.seconds)
			}
		})
	}
}

func TestTrackLengthOf_RefusesALengthNoRecordingCanHave(t *testing.T) {
	tests := []struct {
		name    string
		seconds float64
	}{
		{"not a number", math.NaN()},
		{"infinite", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
		{"negative", -0.5},
		{"just over seven days", 7*24*60*60 + 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kernel.TrackLengthOf(tt.seconds)
			if !errors.Is(err, kernel.ErrImplausibleTrackLength) {
				t.Errorf("TrackLengthOf(%v) error = %v, want ErrImplausibleTrackLength", tt.seconds, err)
			}
			if got.Known() {
				t.Errorf("TrackLengthOf(%v) gave a known length with the error, want the unknown length", tt.seconds)
			}
		})
	}
}

func TestTrackLength_SameRecordingAllowsFiveSecondsOrThreePercentOfTheExpectedLength(t *testing.T) {
	tests := []struct {
		name     string
		expected float64
		actual   float64
		want     bool
	}{
		{"identical", 204, 204, true},
		{"five seconds long on a short track", 100, 105, true},
		{"five seconds short on a short track", 100, 95, true},
		{"just past five seconds long on a short track", 100, 105.1, false},
		{"just past five seconds short on a short track", 100, 94.9, false},
		{"three percent long on a long track", 400, 412, true},
		{"three percent short on a long track", 400, 388, true},
		{"just past three percent long on a long track", 400, 412.1, false},
		{"just past three percent short on a long track", 400, 387.9, false},
		{"just inside five seconds where three percent meets the floor", 500.0 / 3, 500.0/3 + 4.999, true},
		{"just past five seconds where three percent meets the floor", 500.0 / 3, 500.0/3 + 5.001, false},
		{"three percent of the longer expected length covers the gap", 420, 407.5, true},
		{"three percent of the shorter expected length does not", 407.5, 420, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lengthOf(t, tt.expected).SameRecording(lengthOf(t, tt.actual)); got != tt.want {
				t.Errorf("%v s SameRecording(%v s) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

func TestTrackLength_AnUnknownLengthIsNeverTheSameRecording(t *testing.T) {
	tests := []struct {
		name     string
		expected float64
		actual   float64
	}{
		{"unknown expected length", 0, 3},
		{"unknown actual length", 3, 0},
		{"both unknown", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if lengthOf(t, tt.expected).SameRecording(lengthOf(t, tt.actual)) {
				t.Errorf("%v s SameRecording(%v s) = true, want false", tt.expected, tt.actual)
			}
		})
	}
}

func lengthOf(t *testing.T, seconds float64) kernel.TrackLength {
	t.Helper()
	l, err := kernel.TrackLengthOf(seconds)
	if err != nil {
		t.Fatalf("TrackLengthOf(%v): %v", seconds, err)
	}
	return l
}
