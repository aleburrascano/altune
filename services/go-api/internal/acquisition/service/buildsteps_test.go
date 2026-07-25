package service

import (
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

func stepNames(steps []Step) []string {
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name()
	}
	return names
}

func assertStepOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("step count = %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step[%d] = %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSteps_RunsAllSixInOrder(t *testing.T) {
	s := &AcquireTrackAudioService{}
	got := stepNames(s.buildSteps(shared.UserId{}, domain.TrackId{}))
	assertStepOrder(t, got, []string{"search", "select", "download", "tag", "store", "update_track"})
}
