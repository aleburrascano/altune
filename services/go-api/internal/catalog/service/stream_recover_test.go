package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
)

// TestStreamTrackService_RecoverIfMissing covers the presigned-playback recovery
// hook: a genuinely-gone file is marked failed and re-acquired, a present file is
// a no-op (so transient errors don't spuriously re-acquire), and non-streamable
// tracks and exists-check errors are handled.
func TestStreamTrackService_RecoverIfMissing(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	t.Run("missing file marks failed and schedules", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore() // not seeded -> Exists false
		sched := &catalogtest.Scheduler{}
		track := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/gone.opus")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.AcquisitionStatus != domain.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.AcquisitionStatus)
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 scheduled re-acquisition, got %d", len(sched.TrackIds))
		}
	})

	t.Run("present file is a no-op", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		store.Seed("audio/here.opus", []byte("data"))
		sched := &catalogtest.Scheduler{}
		track := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/here.opus")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.AcquisitionStatus != domain.AcquisitionReady {
			t.Errorf("status = %v, want ready (unchanged)", got.AcquisitionStatus)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a present file, got %d", len(sched.TrackIds))
		}
	})

	t.Run("non-streamable track is a no-op", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		sched := &catalogtest.Scheduler{}
		track := seedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a non-streamable track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("exists-check error propagates", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		store.ErrOnExists = errors.New("storage down")
		sched := &catalogtest.Scheduler{}
		track := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/err.opus")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err == nil {
			t.Fatal("expected an error")
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling on exists error, got %d", len(sched.TrackIds))
		}
	})
}
