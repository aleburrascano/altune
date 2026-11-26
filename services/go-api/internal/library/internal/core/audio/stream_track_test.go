package audio_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const addTrackScheduleBound = 5 * time.Second

func TestStreamTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name          string
		setup         func(*library.InMemoryTrackRepo, *library.InMemoryAudioStore) library.TrackId
		wantErr       error
		wantOutput    bool
		wantStatus    *library.DownloadStatus
		wantScheduled bool
	}{
		{
			name: "ready track with present audio streams",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/ok.opus")
				store.Seed("audio/ok.opus", []byte("data"))
				return track.ID
			},
			wantOutput:    true,
			wantStatus:    ptrStatus(library.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "ready track with missing file is marked failed and reacquired",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				return track.ID
			},
			wantErr:       library.ErrAudioNotAvailable,
			wantStatus:    ptrStatus(library.AcquisitionFailed),
			wantScheduled: true,
		},
		{
			name: "missing file whose failed-status persist fails is not reacquired",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				trRepo.ErrOnUpdate = errors.New("db down")
				return track.ID
			},
			wantErr:       library.ErrAudioNotAvailable,
			wantScheduled: false,
		},
		{
			name: "transient stream error over present file is retryable, not missing",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/here.opus")
				store.Seed("audio/here.opus", []byte("data"))
				store.ErrOnStream = errors.New("transient")
				return track.ID
			},
			wantErr:       library.ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(library.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "exists check error is retryable and does not mark failed",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/err.opus")
				store.ErrOnStream = errors.New("stream fail")
				store.ErrOnExists = errors.New("s3 down")
				return track.ID
			},
			wantErr:       library.ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(library.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "pending track is not streamable",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr:       library.ErrAudioNotAvailable,
			wantStatus:    ptrStatus(library.AcquisitionPending),
			wantScheduled: false,
		},
		{
			name: "track not found",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				return library.NewTrackId()
			},
			wantErr:       library.ErrTrackNotFound,
			wantScheduled: false,
		},
		{
			name: "repo GetByID error propagates",
			setup: func(trRepo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) library.TrackId {
				trRepo.ErrOnGetBy = errRepo
				return library.NewTrackId()
			},
			wantErr:       errRepo,
			wantScheduled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trRepo := library.NewInMemoryTrackRepo()
			store := library.NewInMemoryAudioStore()
			sched := &library.InMemoryScheduler{}
			trackId := tt.setup(trRepo, store)
			svc := library.NewStreamTrackService(trRepo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

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
				if track.DownloadStatus != *tt.wantStatus {
					t.Errorf("AcquisitionStatus = %v, want %v", track.DownloadStatus, *tt.wantStatus)
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

func TestStreamTrackService_RecoveryMetric(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	repo := library.NewInMemoryTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := library.NewInMemoryAudioStore()
	store.ErrOnStream = errors.New("not found")
	sched := &library.InMemoryScheduler{}
	metrics := &library.InMemoryAudioStoreMetrics{}
	svc := library.NewStreamTrackService(repo, store, sched, metrics, library.AlwaysEnabled, clock.Now)

	if _, err := svc.Execute(ctx, userId, track.ID); !errors.Is(err, library.ErrAudioNotAvailable) {
		t.Fatalf("error = %v, want ErrAudioNotAvailable", err)
	}
	if metrics.StreamRecoveries != 1 {
		t.Errorf("stream-recovery metric = %d, want 1 when a missing object triggers recovery", metrics.StreamRecoveries)
	}
}

func TestStreamTrackService_NoRecoveryMetricOnHealthyStream(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	repo := library.NewInMemoryTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := library.NewInMemoryAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &library.InMemoryAudioStoreMetrics{}
	svc := library.NewStreamTrackService(repo, store, library.NoopAcquisitionScheduler(), metrics, library.AlwaysEnabled, clock.Now)

	if _, err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.StreamRecoveries != 0 {
		t.Errorf("stream-recovery metric = %d, want 0 on a healthy stream", metrics.StreamRecoveries)
	}
}

func TestStreamTrackService_RecoverIfMissing(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	t.Run("missing file marks failed and schedules", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		sched := &library.InMemoryScheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.DownloadStatus != library.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.DownloadStatus)
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 scheduled re-acquisition, got %d", len(sched.TrackIds))
		}
	})

	t.Run("present file is a no-op", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		store.Seed("audio/here.opus", []byte("data"))
		sched := &library.InMemoryScheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/here.opus")
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.DownloadStatus != library.AcquisitionReady {
			t.Errorf("status = %v, want ready (unchanged)", got.DownloadStatus)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a present file, got %d", len(sched.TrackIds))
		}
	})

	t.Run("missing or foreign track is not found", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		sched := &library.InMemoryScheduler{}
		other := librarytest.OtherUserId()
		foreign := librarytest.SeedReadyTrack(t, repo, other, "Theirs", "Artist", "Album", "audio/theirs.opus")
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		for _, id := range []library.TrackId{library.NewTrackId(), foreign.ID} {
			if err := svc.RecoverIfMissing(ctx, userId, id); !errors.Is(err, library.ErrTrackNotFound) {
				t.Fatalf("error = %v, want ErrTrackNotFound", err)
			}
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a not-found track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("non-streamable track is a no-op", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		sched := &library.InMemoryScheduler{}
		track := librarytest.SeedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a non-streamable track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("exists-check error propagates", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		store.ErrOnExists = errors.New("storage down")
		sched := &library.InMemoryScheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/err.opus")
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err == nil {
			t.Fatal("expected an error")
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling on exists error, got %d", len(sched.TrackIds))
		}
	})

	t.Run("persist failure is returned and does not schedule", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		sched := &library.InMemoryScheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		errUpdate := errors.New("db down")
		repo.ErrOnUpdate = errUpdate
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		err := svc.RecoverIfMissing(ctx, userId, track.ID)
		if !errors.Is(err, errUpdate) {
			t.Fatalf("error = %v, want wrapping %v", err, errUpdate)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling when the persist failed, got %d", len(sched.TrackIds))
		}
	})

	t.Run("scheduler refusal is logged, not returned", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		store := library.NewInMemoryAudioStore()
		sched := &library.InMemoryScheduler{Err: errors.New("queue full")}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := library.NewStreamTrackService(repo, store, sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.DownloadStatus != library.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.DownloadStatus)
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 schedule attempt, got %d", len(sched.TrackIds))
		}
	})
}

func TestStreamTrackService_ReacquireBoundedByTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		userId := librarytest.UserId()
		repo := library.NewInMemoryTrackRepo()
		sched := &library.InMemoryStuckScheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := library.NewStreamTrackService(repo, library.NewInMemoryAudioStore(), sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)

		start := time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		_ = svc.RecoverIfMissing(ctx, userId, track.ID)
		library.AssertScheduleDeadline(t, sched, start, addTrackScheduleBound, clock.Now)
	})
}

func ptrStatus(s library.DownloadStatus) *library.DownloadStatus {
	return &s
}

func TestNewStreamTrackService_RefusesAMissingCollaborator(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	store := library.NewInMemoryAudioStore()
	for name, build := range map[string]func(){
		"no scheduler": func() {
			library.NewStreamTrackService(repo, store, nil, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, clock.Now)
		},
		"no metrics": func() {
			library.NewStreamTrackService(repo, store, library.NoopAcquisitionScheduler(), nil, library.AlwaysEnabled, clock.Now)
		},
		"no recovery switch": func() {
			library.NewStreamTrackService(repo, store, library.NoopAcquisitionScheduler(), library.NoopAudioStoreMetrics(), nil, clock.Now)
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("construction succeeded, want a panic at construction")
				}
			}()
			build()
		})
	}
}

func TestNewStreamTrackService_RefusesANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewStreamTrackService with a nil clock did not panic, want a refusal at construction")
		}
	}()
	library.NewStreamTrackService(library.NewInMemoryTrackRepo(), library.NewInMemoryAudioStore(), &library.InMemoryScheduler{}, library.NoopAudioStoreMetrics(), library.AlwaysEnabled, nil)
}
