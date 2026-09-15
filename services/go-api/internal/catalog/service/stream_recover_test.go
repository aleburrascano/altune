package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
)

func TestStreamTrackService_RecoverIfMissing(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	t.Run("missing file marks failed and schedules", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		sched := &catalogtest.Scheduler{}
		track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
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
		track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/here.opus")
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

	t.Run("missing or foreign track is not found", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		sched := &catalogtest.Scheduler{}
		other := testOtherUserId()
		foreign := seedReadyTrack(t, repo, other, "Theirs", "Artist", "Album", "audio/theirs.opus")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		for _, id := range []domain.TrackId{domain.NewTrackId(), foreign.ID} {
			if err := svc.RecoverIfMissing(ctx, userId, id); !errors.Is(err, ErrTrackNotFound) {
				t.Fatalf("error = %v, want ErrTrackNotFound", err)
			}
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a not-found track, got %d", len(sched.TrackIds))
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
		track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/err.opus")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err == nil {
			t.Fatal("expected an error")
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling on exists error, got %d", len(sched.TrackIds))
		}
	})

	// #1048: scheduling over an unpersisted failed-status would race a new
	// acquisition job against the stale stored row, so a persist failure must
	// not schedule.
	t.Run("persist failure is returned and does not schedule", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		sched := &catalogtest.Scheduler{}
		track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		errUpdate := errors.New("db down")
		repo.ErrOnUpdate = errUpdate
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		err := svc.RecoverIfMissing(ctx, userId, track.ID)
		if !errors.Is(err, errUpdate) {
			t.Fatalf("error = %v, want wrapping %v", err, errUpdate)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling when the persist failed, got %d", len(sched.TrackIds))
		}
	})

	t.Run("scheduler refusal is logged, not returned", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		store := catalogtest.NewAudioStore()
		sched := &catalogtest.Scheduler{Err: errors.New("queue full")}
		track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.AcquisitionStatus != domain.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.AcquisitionStatus)
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 schedule attempt, got %d", len(sched.TrackIds))
		}
	})
}
