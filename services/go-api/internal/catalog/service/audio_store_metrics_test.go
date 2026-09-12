package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"context"
	"errors"
	"testing"
)

func TestDeleteTrackService_OrphanedDeleteMetric(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := catalogtest.NewAudioStore()
	store.ErrOnDelete = errors.New("s3 down")
	metrics := &catalogtest.Metrics{}
	svc := NewDeleteTrackService(repo, store, WithDeleteTrackMetrics(metrics))

	// The track row is deleted even though its audio object is orphaned, so the
	// call succeeds while the orphaned-delete counter must flag the degradation.
	if err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.OrphanedDeletes != 1 {
		t.Errorf("orphaned-delete metric = %d, want 1 so operators can alert on silent orphaning", metrics.OrphanedDeletes)
	}
}

func TestDeleteTrackService_NoOrphanMetricOnCleanDelete(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := catalogtest.NewAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &catalogtest.Metrics{}
	svc := NewDeleteTrackService(repo, store, WithDeleteTrackMetrics(metrics))

	if err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.OrphanedDeletes != 0 {
		t.Errorf("orphaned-delete metric = %d, want 0 on a clean delete", metrics.OrphanedDeletes)
	}
}

func TestStreamTrackService_RecoveryMetric(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := catalogtest.NewAudioStore()
	store.ErrOnStream = errors.New("not found")
	sched := &catalogtest.Scheduler{}
	metrics := &catalogtest.Metrics{}
	svc := NewStreamTrackService(repo, store, WithStreamScheduler(sched), WithStreamMetrics(metrics))

	if _, err := svc.Execute(ctx, userId, track.ID); !errors.Is(err, ErrAudioNotAvailable) {
		t.Fatalf("error = %v, want ErrAudioNotAvailable", err)
	}
	if metrics.StreamRecoveries != 1 {
		t.Errorf("stream-recovery metric = %d, want 1 when a missing object triggers recovery", metrics.StreamRecoveries)
	}
}

func TestStreamTrackService_NoRecoveryMetricOnHealthyStream(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := catalogtest.NewAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &catalogtest.Metrics{}
	svc := NewStreamTrackService(repo, store, WithStreamMetrics(metrics))

	if _, err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.StreamRecoveries != 0 {
		t.Errorf("stream-recovery metric = %d, want 0 on a healthy stream", metrics.StreamRecoveries)
	}
}
