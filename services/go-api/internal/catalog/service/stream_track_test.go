package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStreamTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name          string
		setup         func(*catalogtest.TrackRepo, *catalogtest.AudioStore) domain.TrackId
		wantErr       error
		wantOutput    bool
		wantStatus    *domain.AcquisitionStatus
		wantScheduled bool
	}{
		{
			name: "ready track with present audio streams",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/ok.opus")
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
				track := seedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				return track.ID
			},
			wantErr:       ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionFailed),
			wantScheduled: true,
		},
		{
			name: "missing file whose failed-status persist fails is not reacquired",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				trRepo.ErrOnUpdate = errors.New("db down")
				return track.ID
			},
			wantErr:       ErrAudioNotAvailable,
			wantScheduled: false,
		},
		{
			name: "transient stream error over present file is retryable, not missing",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/here.opus")
				store.Seed("audio/here.opus", []byte("data"))
				store.ErrOnStream = errors.New("transient")
				return track.ID
			},
			wantErr:       ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "exists check error is retryable and does not mark failed",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/err.opus")
				store.ErrOnStream = errors.New("stream fail")
				store.ErrOnExists = errors.New("s3 down")
				return track.ID
			},
			wantErr:       ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "pending track is not streamable",
			setup: func(trRepo *catalogtest.TrackRepo, store *catalogtest.AudioStore) domain.TrackId {
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
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
				if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
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

func TestStreamTrackService_ReacquireBoundedByTimeout(t *testing.T) {
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	sched := &stuckScheduler{}
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	svc := NewStreamTrackService(repo, catalogtest.NewAudioStore(), WithStreamScheduler(sched))

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_ = svc.RecoverIfMissing(ctx, userId, track.ID)
	assertScheduleDeadline(t, sched, start)
}
