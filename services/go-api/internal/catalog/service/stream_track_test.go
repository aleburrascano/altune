package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
)

// TestStreamTrackService_Execute covers the streaming happy path and the
// missing-audio recovery folded in from the former ReconcileTrackStatusService:
// a ready track whose file is gone is marked failed and re-acquisition is
// scheduled, while a transient stream error over a present file leaves the track
// ready. The track is loaded once; recovery acts on that same instance.
func TestStreamTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name          string
		setup         func(*catalogtest.TrackRepo, *catalogtest.AudioStore) domain.TrackId
		wantErr       error
		wantOutput    bool
		wantStatus    *domain.AcquisitionStatus // nil = don't check
		wantScheduled bool
	}{
		{
			name: "ready track with present audio streams",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/ok.opus")
				store.Seed("audio/ok.opus", []byte("data"))
				return track.ID
			},
			wantOutput:    true,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "ready track with missing file is marked failed and reacquired",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				// file not seeded -> Exists returns false
				return track.ID
			},
			wantErr:       ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionFailed),
			wantScheduled: true,
		},
		{
			name: "transient stream error over present file stays ready",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/here.opus")
				store.Seed("audio/here.opus", []byte("data"))
				store.ErrOnStream = errors.New("transient")
				return track.ID
			},
			wantErr:       ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: true,
		},
		{
			name: "exists check error does not mark failed",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/err.opus")
				store.ErrOnStream = errors.New("stream fail")
				store.ErrOnExists = errors.New("s3 down")
				return track.ID
			},
			wantErr:       ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: true,
		},
		{
			name: "pending track is not streamable",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				return track.ID
			},
			wantErr:       ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionPending),
			wantScheduled: false,
		},
		{
			name: "track not found",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				return domain.NewTrackId()
			},
			wantErr:       ErrTrackNotFound,
			wantScheduled: false,
		},
		{
			name: "repo GetByID error propagates",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				trRepo.ErrOnGetBy = errRepo
				return domain.NewTrackId()
			},
			wantErr:       errRepo,
			wantScheduled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trRepo := catalogtest.NewTrackRepo()
			store := catalogtest.NewAudioStore()
			sched := &catalogtest.Scheduler{}
			trackId := tt.setup(trRepo, store)
			svc := NewStreamTrackService(trRepo, store, WithStreamScheduler(sched))

			out, err := svc.Execute(ctx, userId, trackId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantOutput && out == nil {
				t.Error("expected stream output, got nil")
			}
			if !tt.wantOutput && out != nil {
				t.Error("expected nil output")
			}

			if tt.wantStatus != nil {
				track, _ := trRepo.GetByID(ctx, trackId, userId)
				if track == nil {
					t.Fatal("expected track to still exist in repo")
				}
				if track.AcquisitionStatus != *tt.wantStatus {
					t.Errorf("AcquisitionStatus = %v, want %v", track.AcquisitionStatus, *tt.wantStatus)
				}
			}

			if tt.wantScheduled && len(sched.TrackIds) == 0 {
				t.Error("expected re-acquisition to be scheduled")
			}
			if !tt.wantScheduled && len(sched.TrackIds) > 0 {
				t.Errorf("expected no scheduling, got %d", len(sched.TrackIds))
			}
		})
	}
}

// The save flow forwards the discovered source URL to the acquisition scheduler
// so acquisition downloads that exact track (acquire-soundcloud). Relocated from
// the handler suite when scheduling moved into the service.
func TestAddTrackService_ForwardsSourceURLToScheduler(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	sched := &catalogtest.Scheduler{}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(sched))

	scURL := "https://soundcloud.com/liltecca/fell-in-love"
	if _, err := svc.Execute(ctx, userId, AddTrackInput{
		Title:     "Fell In Love",
		Artist:    "Lil Tecca",
		SourceURL: &scURL,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sched.SourceURLs) != 1 || sched.SourceURLs[0] != scURL {
		t.Fatalf("scheduler should receive source URL %q, got %v", scURL, sched.SourceURLs)
	}
}

// A save with no source URL forwards an empty string (acquisition falls back to
// search).
func TestAddTrackService_NoSourceURL_ForwardsEmpty(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	sched := &catalogtest.Scheduler{}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(sched))

	if _, err := svc.Execute(ctx, userId, AddTrackInput{Title: "Some Track", Artist: "Some Artist"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sched.SourceURLs) != 1 || sched.SourceURLs[0] != "" {
		t.Fatalf("scheduler should receive an empty source URL, got %v", sched.SourceURLs)
	}
}
