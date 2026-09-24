package domain

import "testing"

func TestRepeatMode_RoundTrip(t *testing.T) {
	for _, rm := range []RepeatMode{RepeatOff, RepeatAll, RepeatOne} {
		parsed, err := ParseRepeatMode(rm.String())
		if err != nil {
			t.Fatalf("ParseRepeatMode(%q): %v", rm.String(), err)
		}
		if parsed != rm {
			t.Errorf("round-trip %v -> %q -> %v", rm, rm.String(), parsed)
		}
	}
}
