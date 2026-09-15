package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestReacquirePolicy_Reconcile pins every outcome of reconcile so the
// Ready-branch exists-check can be restructured without changing behavior.
func TestReacquirePolicy_Reconcile(t *testing.T) {
	t.Parallel()
	const audioRef = "user/artist/album/song.mp3"
	existsErr := errors.New("transient s3 hiccup")
	updateErr := errors.New("db down")

	tests := []struct {
		name        string
		status      domain.AcquisitionStatus
		audioRef    *string
		stored      bool
		existsErr   error
		updateErr   error
		wantProceed bool
		wantErrIs   error
		wantStatus  domain.AcquisitionStatus
		wantUpdated bool
	}{
		{name: "ready exists check fails", status: domain.AcquisitionReady, audioRef: refPtr(audioRef), existsErr: existsErr, wantErrIs: existsErr, wantStatus: domain.AcquisitionReady},
		{name: "ready file exists", status: domain.AcquisitionReady, audioRef: refPtr(audioRef), stored: true, wantStatus: domain.AcquisitionReady},
		{name: "ready file missing", status: domain.AcquisitionReady, audioRef: refPtr(audioRef), wantProceed: true, wantStatus: domain.AcquisitionPending, wantUpdated: true},
		{name: "ready without audio ref", status: domain.AcquisitionReady, wantProceed: true, wantStatus: domain.AcquisitionPending, wantUpdated: true},
		{name: "ready file missing revert fails", status: domain.AcquisitionReady, audioRef: refPtr(audioRef), updateErr: updateErr, wantErrIs: updateErr, wantStatus: domain.AcquisitionPending},
		{name: "failed retries", status: domain.AcquisitionFailed, wantProceed: true, wantStatus: domain.AcquisitionPending, wantUpdated: true},
		{name: "failed revert fails", status: domain.AcquisitionFailed, updateErr: updateErr, wantErrIs: updateErr, wantStatus: domain.AcquisitionPending},
		{name: "pending proceeds untouched", status: domain.AcquisitionPending, wantProceed: true, wantStatus: domain.AcquisitionPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Song", "Artist", "Album")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			track.AcquisitionStatus = tt.status
			track.AudioRef = tt.audioRef

			repo := newFakeTrackRepository()
			repo.err = tt.updateErr
			store := newFakeAudioStore()
			store.err = tt.existsErr
			if tt.stored {
				store.stored[audioRef] = true
			}

			proceed, err := reacquirePolicy{trackRepo: repo, audioStore: store}.reconcile(context.Background(), track)

			if !errors.Is(err, tt.wantErrIs) || (tt.wantErrIs == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErrIs)
			}
			if proceed != tt.wantProceed {
				t.Errorf("proceed = %v, want %v", proceed, tt.wantProceed)
			}
			if track.AcquisitionStatus != tt.wantStatus {
				t.Errorf("status = %v, want %v", track.AcquisitionStatus, tt.wantStatus)
			}
			if got := len(repo.tracks) == 1; got != tt.wantUpdated {
				t.Errorf("persisted = %v, want %v", got, tt.wantUpdated)
			}
		})
	}
}

func refPtr(s string) *string { return &s }
