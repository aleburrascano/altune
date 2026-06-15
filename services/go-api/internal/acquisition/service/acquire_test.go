package service

import (
	"context"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestAcquireTrackAudioService_Execute_TrackNotFound(t *testing.T) {
	// Arrange: empty repo — GetByID returns nil
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	// Act
	err := svc.Execute(context.Background(), userId, trackId)

	// Assert: silent no-op, returns nil
	if err != nil {
		t.Fatalf("expected nil for track-not-found (silent no-op), got %v", err)
	}
}

func TestAcquireTrackAudioService_Execute_AlreadyReady_AudioExists(t *testing.T) {
	// Arrange: track is ready, audio file exists in store
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()
	store.stored[audioRef] = true // audio file exists

	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	// Act
	execErr := svc.Execute(context.Background(), userId, track.ID)

	// Assert: skip, return nil
	if execErr != nil {
		t.Fatalf("expected nil for already-ready track with existing audio, got %v", execErr)
	}

	// Track should still be ready (not reverted)
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v (should remain ready)", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
}

func TestAcquireTrackAudioService_Execute_AlreadyReady_AudioMissing(t *testing.T) {
	// Arrange: track is ready, but audio file is NOT in store
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()
	// store.stored is empty — audio file doesn't exist

	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	// Act: Execute will revert to pending, then try to re-acquire.
	// Since the fake searcher returns no results, the pipeline will fail.
	// We just care that the track was reverted to pending before the pipeline runs.
	_ = svc.Execute(context.Background(), userId, track.ID)

	// Assert: track should have been reverted to pending (then pipeline ran and failed,
	// which marks it as failed via markFailed). Either pending or failed is acceptable —
	// the key assertion is that it was NOT left in "ready" with no audio.
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus == domain.AcquisitionReady {
		t.Error("track should not remain in 'ready' status when audio file is missing")
	}
}

func TestAcquireTrackAudioService_Execute_FailedStatus_RetriesToAcquire(t *testing.T) {
	// Arrange: track is in failed status
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	_ = track.MarkFailed("previous failure reason")

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()
	searcher := &fakeAudioSearcher{} // no results → pipeline will fail
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	// Act
	_ = svc.Execute(context.Background(), userId, track.ID)

	// Assert: the track should have been reverted from failed state.
	// Since searcher returns nothing, it will be marked failed again,
	// but the key behavior is that it attempted re-acquisition (didn't bail out).
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus == domain.AcquisitionPending {
		// If it stayed pending, revert happened but markFailed didn't run — that's
		// also acceptable (means pipeline error path didn't re-mark). Either way,
		// it did NOT stay in the original failed state with "previous failure reason".
	}
	// At minimum, the original failure reason should be replaced
	if updated.FailureReason != nil && *updated.FailureReason == "previous failure reason" {
		t.Error("expected failure reason to change after retry attempt, but it remained the original")
	}
}
