package service_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

const reacquireRef = "user/artist/album/song.mp3"

type reacquireSource struct{ finds atomic.Int32 }

func (s *reacquireSource) Name() domain.SourceName { return "reacquire" }

func (s *reacquireSource) Find(context.Context, domain.FindRequest) ([]domain.AudioCandidate, error) {
	s.finds.Add(1)
	return nil, nil
}

func (s *reacquireSource) Fetch(context.Context, domain.AudioCandidate, string) (domain.FetchedAudio, error) {
	return domain.FetchedAudio{}, errors.New("nothing to fetch")
}

type reacquireStore struct {
	stored    bool
	existsErr error
}

func (s reacquireStore) Exists(_ context.Context, audioRef string) (bool, error) {
	return s.stored && audioRef == reacquireRef, s.existsErr
}

func (reacquireStore) Store(context.Context, string, string) error { return nil }

func (reacquireStore) Delete(context.Context, string) error { return nil }

func TestExecute_ReconcilesTheTracksStatusBeforeItSearches(t *testing.T) {
	t.Parallel()
	existsErr := errors.New("transient s3 hiccup")
	writeErr := errors.New("db down")
	ref := reacquireRef
	noMatch := string(music.FailureNoMatchFound)

	tests := []struct {
		name        string
		status      domain.TrackStatus
		audioRef    *string
		streamable  bool
		store       reacquireStore
		writeErr    error
		wantErrIs   error
		wantSearch  bool
		wantStatus  domain.TrackStatus
		wantReason  string
		wantVersion int
	}{
		{name: "ready exists check fails", status: domain.TrackReady, audioRef: &ref, streamable: true, store: reacquireStore{existsErr: existsErr}, wantErrIs: existsErr, wantStatus: domain.TrackReady},
		{name: "ready file exists", status: domain.TrackReady, audioRef: &ref, streamable: true, store: reacquireStore{stored: true}, wantStatus: domain.TrackReady},
		{name: "ready file missing", status: domain.TrackReady, audioRef: &ref, streamable: true, wantSearch: true, wantStatus: domain.TrackFailed, wantReason: noMatch, wantVersion: 2},
		{name: "ready without audio ref", status: domain.TrackReady, wantSearch: true, wantStatus: domain.TrackFailed, wantReason: noMatch, wantVersion: 2},
		{name: "ready file missing revert fails", status: domain.TrackReady, audioRef: &ref, streamable: true, writeErr: writeErr, wantErrIs: writeErr, wantStatus: domain.TrackReady},
		{name: "failed retries", status: domain.TrackFailed, wantSearch: true, wantStatus: domain.TrackFailed, wantReason: noMatch, wantVersion: 2},
		{name: "failed revert fails", status: domain.TrackFailed, writeErr: writeErr, wantErrIs: writeErr, wantStatus: domain.TrackFailed, wantReason: "download_failed"},
		{name: "pending proceeds untouched", status: domain.TrackPending, wantSearch: true, wantStatus: domain.TrackFailed, wantReason: noMatch, wantVersion: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := memory.LedgerTrack{TrackSnapshot: domain.TrackSnapshot{
				ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
				Title: "Song", Artist: "Artist", Album: "Album", Status: tt.status, Streamable: tt.streamable, AudioRef: tt.audioRef,
			}}
			if tt.status == domain.TrackFailed {
				track.FailureReason = "download_failed"
			}
			ledger := memory.NewLedger(track)
			ledger.FailWrites(tt.writeErr)
			source := &reacquireSource{}
			svc := service.MustNewAcquireTrackAudioService(ledger, service.NewSourceRegistry(source), tt.store,
				scratchfiles.New(), noVerification(), noStages())

			err := svc.Execute(context.Background(), domain.AcquisitionJob{UserID: track.UserId, TrackID: track.ID})

			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Fatalf("Execute = %v, want %v", err, tt.wantErrIs)
			}
			if tt.wantErrIs == nil && !tt.wantSearch && err != nil {
				t.Fatalf("Execute = %v, want a track with its file kept to settle quietly", err)
			}
			if searched := source.finds.Load() > 0; searched != tt.wantSearch {
				t.Errorf("searched = %v, want %v", searched, tt.wantSearch)
			}
			got, _ := ledger.Track(track.ID)
			if got.Status != tt.wantStatus || got.FailureReason != tt.wantReason || got.Version != tt.wantVersion {
				t.Errorf("track = status %v reason %q version %d, want status %v reason %q version %d",
					got.Status, got.FailureReason, got.Version, tt.wantStatus, tt.wantReason, tt.wantVersion)
			}
		})
	}
}
