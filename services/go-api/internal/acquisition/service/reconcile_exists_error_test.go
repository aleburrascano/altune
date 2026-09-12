package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestReconcileForReacquire_ExistsError_PreservesAudioRef(t *testing.T) {
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
	store.err = errors.New("transient s3 hiccup")

	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), store)

	proceed, reconcileErr := svc.reconcileForReacquire(context.Background(), track)

	if reconcileErr == nil {
		t.Fatal("expected reconcileForReacquire to return the exists-check error, got nil")
	}
	if proceed {
		t.Error("expected proceed=false when the exists check errors")
	}
	if track.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want %v (must stay ready on transient error)", track.AcquisitionStatus, domain.AcquisitionReady)
	}
	if track.AudioRef == nil || *track.AudioRef != audioRef {
		t.Errorf("AudioRef = %v, want %q preserved (a transient error is not evidence the file is gone)", track.AudioRef, audioRef)
	}
}
