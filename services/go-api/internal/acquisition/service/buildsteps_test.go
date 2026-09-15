package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

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

// acquirableFixture is a real track plus fakes under which every real stage
// succeeds, so the whole assembled pipeline runs end to end.
func acquirableFixture(t *testing.T) (*AcquireTrackAudioService, *domain.Track, *AcquisitionContext) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	audio := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			URL:        "https://youtube.com/watch?v=topic1",
			Categories: []string{"Music"},
		}},
		downloadPath: audio,
	}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), newFakeAudioStore())
	ac := &AcquisitionContext{Track: TrackRef{
		ID: track.ID.String(), UserID: userId.String(),
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours", Duration: 200,
	}}
	return svc, track, ac
}

// TestBuildSteps_RunsAllSixInOrder runs the production assembly for real and
// records the stages RunPipeline actually executed, so it proves execution
// order rather than a declared list. The order itself is fixed by the stage
// token types; this pins the observable result.
func TestBuildSteps_RunsAllSixInOrder(t *testing.T) {
	svc, track, ac := acquirableFixture(t)
	rep := &recordingReporter{}
	ctx := withJobReporter(context.Background(), rep)

	if err := RunPipeline(ctx, svc.buildSteps(track.UserId, track.ID), ac); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	CleanupTemp(ctx, ac)
	assertStepOrder(t, rep.stages, []string{"search", "select", "download", "tag", "store", "update_track"})
}

func TestCoreSteps_StopsAfterStore(t *testing.T) {
	svc, _, ac := acquirableFixture(t)
	rep := &recordingReporter{}
	ctx := withJobReporter(context.Background(), rep)

	core := CoreSteps(svc.sources, svc.audioTagger, svc.audioStore, svc.audioProber, svc.identifier)
	if err := RunPipeline(ctx, core, ac); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	CleanupTemp(ctx, ac)
	assertStepOrder(t, rep.stages, []string{"search", "select", "download", "tag", "store"})
	if ac.AudioRef == "" {
		t.Error("store stage did not record an audio ref")
	}
}
