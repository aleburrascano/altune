package audio_test

import (
	"altune/go-api/internal/catalog"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const addTrackScheduleBound = 5 * time.Second

func TestStreamTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name          string
		setup         func(*catalog.InMemoryTrackRepo, *catalog.InMemoryAudioStore) catalog.TrackId
		wantErr       error
		wantOutput    bool
		wantStatus    *catalog.AcquisitionStatus
		wantScheduled bool
	}{
		{
			name: "ready track with present audio streams",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				track := catalog.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/ok.opus")
				store.Seed("audio/ok.opus", []byte("data"))
				return track.ID
			},
			wantOutput:    true,
			wantStatus:    ptrStatus(catalog.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "ready track with missing file is marked failed and reacquired",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				track := catalog.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				return track.ID
			},
			wantErr:       catalog.ErrAudioNotAvailable,
			wantStatus:    ptrStatus(catalog.AcquisitionFailed),
			wantScheduled: true,
		},
		{
			name: "missing file whose failed-status persist fails is not reacquired",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				track := catalog.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				trRepo.ErrOnUpdate = errors.New("db down")
				return track.ID
			},
			wantErr:       catalog.ErrAudioNotAvailable,
			wantScheduled: false,
		},
		{
			name: "transient stream error over present file is retryable, not missing",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				track := catalog.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/here.opus")
				store.Seed("audio/here.opus", []byte("data"))
				store.ErrOnStream = errors.New("transient")
				return track.ID
			},
			wantErr:       catalog.ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(catalog.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "exists check error is retryable and does not mark failed",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				track := catalog.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/err.opus")
				store.ErrOnStream = errors.New("stream fail")
				store.ErrOnExists = errors.New("s3 down")
				return track.ID
			},
			wantErr:       catalog.ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(catalog.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "pending track is not streamable",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr:       catalog.ErrAudioNotAvailable,
			wantStatus:    ptrStatus(catalog.AcquisitionPending),
			wantScheduled: false,
		},
		{
			name: "track not found",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				return catalog.NewTrackId()
			},
			wantErr:       catalog.ErrTrackNotFound,
			wantScheduled: false,
		},
		{
			name: "repo GetByID error propagates",
			setup: func(trRepo *catalog.InMemoryTrackRepo, store *catalog.InMemoryAudioStore) catalog.TrackId {
				trRepo.ErrOnGetBy = errRepo
				return catalog.NewTrackId()
			},
			wantErr:       errRepo,
			wantScheduled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trRepo := catalog.NewInMemoryTrackRepo()
			store := catalog.NewInMemoryAudioStore()
			sched := &catalog.InMemoryScheduler{}
			trackId := tt.setup(trRepo, store)
			svc := catalog.NewStreamTrackService(trRepo, store, catalog.WithStreamScheduler(sched))

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
	userId := catalog.FixtureUserId()
	repo := catalog.NewInMemoryTrackRepo()
	sched := &catalog.InMemoryScheduler{}
	svc := catalog.NewAddTrackService(repo, catalog.WithAcquisitionScheduler(sched))

	scURL := "https://soundcloud.com/liltecca/fell-in-love"
	if _, err := svc.Execute(ctx, userId, catalog.AddTrackInput{
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
	userId := catalog.FixtureUserId()
	repo := catalog.NewInMemoryTrackRepo()
	sched := &catalog.InMemoryScheduler{}
	svc := catalog.NewAddTrackService(repo, catalog.WithAcquisitionScheduler(sched))

	if _, err := svc.Execute(ctx, userId, catalog.AddTrackInput{Title: "Some Track", Artist: "Some Artist"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sched.SourceURLs) != 1 || sched.SourceURLs[0] != "" {
		t.Fatalf("scheduler should receive an empty source URL, got %v", sched.SourceURLs)
	}
}

func TestStreamTrackService_RecoveryMetric(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()

	repo := catalog.NewInMemoryTrackRepo()
	track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := catalog.NewInMemoryAudioStore()
	store.ErrOnStream = errors.New("not found")
	sched := &catalog.InMemoryScheduler{}
	metrics := &catalog.InMemoryAudioStoreMetrics{}
	svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched), catalog.WithStreamMetrics(metrics))

	if _, err := svc.Execute(ctx, userId, track.ID); !errors.Is(err, catalog.ErrAudioNotAvailable) {
		t.Fatalf("error = %v, want ErrAudioNotAvailable", err)
	}
	if metrics.StreamRecoveries != 1 {
		t.Errorf("stream-recovery metric = %d, want 1 when a missing object triggers recovery", metrics.StreamRecoveries)
	}
}

func TestStreamTrackService_NoRecoveryMetricOnHealthyStream(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()

	repo := catalog.NewInMemoryTrackRepo()
	track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := catalog.NewInMemoryAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &catalog.InMemoryAudioStoreMetrics{}
	svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamMetrics(metrics))

	if _, err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.StreamRecoveries != 0 {
		t.Errorf("stream-recovery metric = %d, want 0 on a healthy stream", metrics.StreamRecoveries)
	}
}

func TestStreamTrackService_RecoverIfMissing(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()

	t.Run("missing file marks failed and schedules", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		sched := &catalog.InMemoryScheduler{}
		track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.AcquisitionStatus != catalog.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.AcquisitionStatus)
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 scheduled re-acquisition, got %d", len(sched.TrackIds))
		}
	})

	t.Run("present file is a no-op", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		store.Seed("audio/here.opus", []byte("data"))
		sched := &catalog.InMemoryScheduler{}
		track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/here.opus")
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.AcquisitionStatus != catalog.AcquisitionReady {
			t.Errorf("status = %v, want ready (unchanged)", got.AcquisitionStatus)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a present file, got %d", len(sched.TrackIds))
		}
	})

	t.Run("missing or foreign track is not found", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		sched := &catalog.InMemoryScheduler{}
		other := catalog.FixtureOtherUserId()
		foreign := catalog.SeedReadyTrack(t, repo, other, "Theirs", "Artist", "Album", "audio/theirs.opus")
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		for _, id := range []catalog.TrackId{catalog.NewTrackId(), foreign.ID} {
			if err := svc.RecoverIfMissing(ctx, userId, id); !errors.Is(err, catalog.ErrTrackNotFound) {
				t.Fatalf("error = %v, want ErrTrackNotFound", err)
			}
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a not-found track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("non-streamable track is a no-op", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		sched := &catalog.InMemoryScheduler{}
		track := catalog.SeedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a non-streamable track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("exists-check error propagates", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		store.ErrOnExists = errors.New("storage down")
		sched := &catalog.InMemoryScheduler{}
		track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/err.opus")
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err == nil {
			t.Fatal("expected an error")
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling on exists error, got %d", len(sched.TrackIds))
		}
	})

	t.Run("persist failure is returned and does not schedule", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		sched := &catalog.InMemoryScheduler{}
		track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		errUpdate := errors.New("db down")
		repo.ErrOnUpdate = errUpdate
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		err := svc.RecoverIfMissing(ctx, userId, track.ID)
		if !errors.Is(err, errUpdate) {
			t.Fatalf("error = %v, want wrapping %v", err, errUpdate)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling when the persist failed, got %d", len(sched.TrackIds))
		}
	})

	t.Run("scheduler refusal is logged, not returned", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		store := catalog.NewInMemoryAudioStore()
		sched := &catalog.InMemoryScheduler{Err: errors.New("queue full")}
		track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := catalog.NewStreamTrackService(repo, store, catalog.WithStreamScheduler(sched))

		if err := svc.RecoverIfMissing(ctx, userId, track.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID, userId)
		if got.AcquisitionStatus != catalog.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.AcquisitionStatus)
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 schedule attempt, got %d", len(sched.TrackIds))
		}
	})
}

func TestStreamTrackService_ReacquireBoundedByTimeout(t *testing.T) {
	userId := catalog.FixtureUserId()
	repo := catalog.NewInMemoryTrackRepo()
	sched := &catalog.InMemoryStuckScheduler{}
	track := catalog.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	svc := catalog.NewStreamTrackService(repo, catalog.NewInMemoryAudioStore(), catalog.WithStreamScheduler(sched))

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_ = svc.RecoverIfMissing(ctx, userId, track.ID)
	catalog.AssertScheduleDeadline(t, sched, start, addTrackScheduleBound)
}

func ptrStatus(s catalog.AcquisitionStatus) *catalog.AcquisitionStatus {
	return &s
}
