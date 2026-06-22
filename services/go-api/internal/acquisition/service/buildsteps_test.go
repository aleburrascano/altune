package service

import (
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// buildsteps_test pins the acquisition pipeline's step ASSEMBLY — the glue that
// RunPipeline's generic test and the per-step tests don't cover. The load-bearing
// invariant: the direct path skips search+select but still runs the shared
// download → tag → store → update_track tail, and the search path runs all six in
// order. A regression here (a reordered or dropped step) would otherwise only
// surface end-to-end.

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

func TestBuildSteps_SearchPathRunsAllSixInOrder(t *testing.T) {
	s := &AcquireTrackAudioService{}
	got := stepNames(s.buildSteps(shared.UserId{}, domain.TrackId{}, false))
	assertStepOrder(t, got, []string{"search", "select", "download", "tag", "store", "update_track"})
}

func TestBuildSteps_DirectPathSkipsSearchAndSelect(t *testing.T) {
	s := &AcquireTrackAudioService{}
	got := stepNames(s.buildSteps(shared.UserId{}, domain.TrackId{}, true))
	// Skips search+select; keeps the shared download → tag → store → update_track tail.
	assertStepOrder(t, got, []string{"download", "tag", "store", "update_track"})
}
