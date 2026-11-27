package domain_test

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/service"
	"testing"
)

func TestRepeatMode_RoundTrip(t *testing.T) {
	for _, rm := range []domain.RepeatMode{domain.RepeatOff, domain.RepeatAll, domain.RepeatOne} {
		saved, err := saveQueue(service.SaveQueueStateInput{RepeatMode: rm.String()})
		if err != nil {
			t.Fatalf("save with repeat mode %q: %v", rm.String(), err)
		}
		if saved.RepeatMode != rm {
			t.Errorf("round-trip %v -> %q -> %v", rm, rm.String(), saved.RepeatMode)
		}
	}
}

func TestRepeatMode_EmptyMeansOff(t *testing.T) {
	saved, err := saveQueue(service.SaveQueueStateInput{})
	if err != nil {
		t.Fatalf("save without a repeat mode: %v", err)
	}
	if saved.RepeatMode != domain.RepeatOff {
		t.Errorf("RepeatMode = %v, want off when the client sends none", saved.RepeatMode)
	}
}
