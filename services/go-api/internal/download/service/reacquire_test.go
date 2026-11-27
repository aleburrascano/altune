package service_test

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/jobrun"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

const reacquireRef = "user/artist/album/song.mp3"

type reacquireSource struct{ finds atomic.Int32 }

func (s *reacquireSource) Name() acqdomain.SourceName { return "reacquire" }

func (s *reacquireSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.finds.Add(1)
	return nil, nil
}

func (s *reacquireSource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("nothing to fetch")
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
	noMatch := string(downloadfailure.FailureNoMatchFound)

	tests := []struct {
		name        string
		status      ports.TrackStatus
		audioRef    *string
		store       reacquireStore
		writeErr    error
		wantErrIs   error
		wantSearch  bool
		wantStatus  ports.TrackStatus
		wantReason  string
		wantVersion int
	}{
		{name: "ready exists check fails", status: ports.TrackReady, audioRef: &ref, store: reacquireStore{existsErr: existsErr}, wantErrIs: existsErr, wantStatus: ports.TrackReady},
		{name: "ready file exists", status: ports.TrackReady, audioRef: &ref, store: reacquireStore{stored: true}, wantStatus: ports.TrackReady},
		{name: "ready file missing", status: ports.TrackReady, audioRef: &ref, wantSearch: true, wantStatus: ports.TrackFailed, wantReason: noMatch, wantVersion: 2},
		{name: "ready without audio ref", status: ports.TrackReady, wantSearch: true, wantStatus: ports.TrackFailed, wantReason: noMatch, wantVersion: 2},
		{name: "ready file missing revert fails", status: ports.TrackReady, audioRef: &ref, writeErr: writeErr, wantErrIs: writeErr, wantStatus: ports.TrackReady},
		{name: "failed retries", status: ports.TrackFailed, wantSearch: true, wantStatus: ports.TrackFailed, wantReason: noMatch, wantVersion: 2},
		{name: "failed revert fails", status: ports.TrackFailed, writeErr: writeErr, wantErrIs: writeErr, wantStatus: ports.TrackFailed, wantReason: "download_failed"},
		{name: "pending proceeds untouched", status: ports.TrackPending, wantSearch: true, wantStatus: ports.TrackFailed, wantReason: noMatch, wantVersion: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := inmemoryledger.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
				ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
				Title: "Song", Artist: "Artist", Album: "Album", Status: tt.status, AudioRef: tt.audioRef,
			}}
			if tt.status == ports.TrackFailed {
				track.FailureReason = "download_failed"
			}
			ledger := inmemoryledger.NewLedger(track)
			ledger.FailWrites(tt.writeErr)
			source := &reacquireSource{}
			svc := service.MustNewAcquireTrackAudioService(ledger, service.NewSourceRegistry(source), tt.store,
				scratchfiles.New(), noVerification(), noStages())

			err := svc.Execute(context.Background(), jobrun.Job{UserID: track.UserId, TrackID: track.ID})

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
