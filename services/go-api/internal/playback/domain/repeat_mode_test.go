package domain_test

import (
	"altune/go-api/internal/playback"
	"testing"
)

func TestRepeatMode_RoundTrip(t *testing.T) {
	for _, rm := range []playback.RepeatMode{playback.RepeatOff, playback.RepeatAll, playback.RepeatOne} {
		saved, err := saveQueue(playback.SaveQueueStateInput{RepeatMode: rm.String()})
		if err != nil {
			t.Fatalf("save with repeat mode %q: %v", rm.String(), err)
		}
		if saved.RepeatMode != rm {
			t.Errorf("round-trip %v -> %q -> %v", rm, rm.String(), saved.RepeatMode)
		}
	}
}

func TestRepeatMode_EmptyMeansOff(t *testing.T) {
	saved, err := saveQueue(playback.SaveQueueStateInput{})
	if err != nil {
		t.Fatalf("save without a repeat mode: %v", err)
	}
	if saved.RepeatMode != playback.RepeatOff {
		t.Errorf("RepeatMode = %v, want off when the client sends none", saved.RepeatMode)
	}
}
