package tracklength_test

import (
	"altune/go-api/internal/shared/tracklength"
	"errors"
	"math"
	"testing"

	"pgregory.net/rapid"
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
			got, err := tracklength.TrackLengthOf(tt.seconds)
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
			got, err := tracklength.TrackLengthOf(tt.seconds)
			if !errors.Is(err, tracklength.ErrImplausibleTrackLength) {
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

const sevenDaysInSeconds = 7 * 24 * 60 * 60

func knownLengthSeconds() *rapid.Generator[float64] {
	return rapid.Float64Range(0.001, sevenDaysInSeconds)
}

func lengthFor(t *rapid.T, seconds float64) tracklength.TrackLength {
	t.Helper()
	l, err := tracklength.TrackLengthOf(seconds)
	if err != nil {
		t.Fatalf("TrackLengthOf(%v): %v", seconds, err)
	}
	return l
}

func TestTrackLengthOf_AcceptsExactlyTheLengthsFromZeroToSevenDays(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		seconds := rapid.OneOf(
			rapid.Float64(),
			rapid.Float64Range(-10, sevenDaysInSeconds+10),
			rapid.Just(math.NaN()),
			rapid.Just(math.Inf(1)),
			rapid.Just(math.Inf(-1)),
		).Draw(t, "seconds")

		got, err := tracklength.TrackLengthOf(seconds)

		plausible := seconds >= 0 && seconds <= sevenDaysInSeconds
		if plausible != (err == nil) {
			t.Fatalf("TrackLengthOf(%v) error = %v, want an error exactly when the length is outside 0 to 7 days", seconds, err)
		}
		if err != nil {
			if !errors.Is(err, tracklength.ErrImplausibleTrackLength) || got.Known() {
				t.Fatalf("TrackLengthOf(%v) = %v, %v, want the unknown length and ErrImplausibleTrackLength", seconds, got, err)
			}
			return
		}
		if got.Seconds() != seconds || got.Known() != (seconds > 0) {
			t.Fatalf("TrackLengthOf(%v) = %v s known=%v, want the same seconds, known exactly when above zero", seconds, got.Seconds(), got.Known())
		}
	})
}

func TestTrackLength_SameRecordingIsReflexiveForAKnownLength(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		length := lengthFor(t, knownLengthSeconds().Draw(t, "seconds"))
		if !length.SameRecording(length) {
			t.Fatalf("%v s is not the same recording as itself", length.Seconds())
		}
	})
}

func TestTrackLength_SameRecordingAcceptsAnyGapUnderFiveSecondsAnywhereOnTheRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		expected := knownLengthSeconds().Draw(t, "expected")
		gap := rapid.Float64Range(0, 4.99).Draw(t, "gap")
		actual := expected + gap
		if rapid.Bool().Draw(t, "shorter") && expected-gap > 0 {
			actual = expected - gap
		}
		if actual > sevenDaysInSeconds {
			t.Skip()
		}
		if !lengthFor(t, expected).SameRecording(lengthFor(t, actual)) {
			t.Fatalf("%v s vs %v s differ by %v s, want the same recording", expected, actual, gap)
		}
	})
}

func TestTrackLength_SameRecordingAcceptsAnyGapUnderThreePercentOfTheExpectedLength(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		expected := knownLengthSeconds().Draw(t, "expected")
		share := rapid.Float64Range(0, 0.0299).Draw(t, "share")
		actual := expected * (1 + share)
		if rapid.Bool().Draw(t, "shorter") {
			actual = expected * (1 - share)
		}
		if actual <= 0 || actual > sevenDaysInSeconds {
			t.Skip()
		}
		if !lengthFor(t, expected).SameRecording(lengthFor(t, actual)) {
			t.Fatalf("%v s vs %v s differ by %v of the expected length, want the same recording", expected, actual, share)
		}
	})
}

func TestTrackLength_SameRecordingRefusesAnyGapBeyondTheLargerOfFiveSecondsAndThreePercent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		expected := rapid.Float64Range(1, sevenDaysInSeconds/2).Draw(t, "expected")
		allowance := math.Max(5, 0.03*expected)
		gap := allowance * (1 + rapid.Float64Range(0.001, 5).Draw(t, "overshoot"))
		actual := expected + gap
		if rapid.Bool().Draw(t, "shorter") && expected-gap > 0 {
			actual = expected - gap
		}
		if lengthFor(t, expected).SameRecording(lengthFor(t, actual)) {
			t.Fatalf("%v s vs %v s differ by %v s, past the %v s allowance, want different recordings", expected, actual, gap, allowance)
		}
	})
}

func TestTrackLength_AMatchStaysAMatchWhenTheActualLengthMovesTowardTheExpectedOne(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		expected := lengthFor(t, knownLengthSeconds().Draw(t, "expected"))
		far := lengthFor(t, knownLengthSeconds().Draw(t, "far"))
		if !expected.SameRecording(far) {
			t.Skip()
		}
		fraction := rapid.Float64Range(0, 0.999).Draw(t, "fraction")
		nearer := lengthFor(t, expected.Seconds()+fraction*(far.Seconds()-expected.Seconds()))
		if !nearer.Known() {
			t.Skip()
		}
		if !expected.SameRecording(nearer) {
			t.Fatalf("%v s matched %v s but not %v s, which is closer on the same side", expected.Seconds(), far.Seconds(), nearer.Seconds())
		}
	})
}

func TestTrackLength_AnUnknownLengthIsNeverTheSameRecordingAsAnyLength(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		unknown := lengthFor(t, 0)
		other := lengthFor(t, rapid.Float64Range(0, sevenDaysInSeconds).Draw(t, "other"))
		if unknown.SameRecording(other) || other.SameRecording(unknown) {
			t.Fatalf("the unknown length matched %v s", other.Seconds())
		}
	})
}

func lengthOf(t *testing.T, seconds float64) tracklength.TrackLength {
	t.Helper()
	l, err := tracklength.TrackLengthOf(seconds)
	if err != nil {
		t.Fatalf("TrackLengthOf(%v): %v", seconds, err)
	}
	return l
}
