package service_test

import (
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestStreamTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name          string
		setup         func(*memory.TrackRepo, *memory.AudioStore) domain.TrackId
		wantErr       error
		wantOutput    bool
		wantStatus    *domain.DownloadStatus
		wantScheduled bool
	}{
		{
			name: "ready track with present audio streams",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/ok.opus")
				store.Seed("audio/ok.opus", []byte("data"))
				return track.ID()
			},
			wantOutput:    true,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "ready track with missing file is marked failed and reacquired",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				return track.ID()
			},
			wantErr:       domain.ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionFailed),
			wantScheduled: true,
		},
		{
			name: "missing file whose failed-status persist fails is not reacquired",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/gone.opus")
				store.ErrOnStream = errors.New("not found")
				trRepo.ErrOnUpdate = errors.New("db down")
				return track.ID()
			},
			wantErr:       domain.ErrAudioNotAvailable,
			wantScheduled: false,
		},
		{
			name: "transient stream error over present file is retryable, not missing",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/here.opus")
				store.Seed("audio/here.opus", []byte("data"))
				store.ErrOnStream = errors.New("transient")
				return track.ID()
			},
			wantErr:       domain.ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "exists check error is retryable and does not mark failed",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				track := librarytest.SeedReadyTrack(t, trRepo, userId, "Track", "Artist", "Album", "audio/err.opus")
				store.ErrOnStream = errors.New("stream fail")
				store.ErrOnExists = errors.New("s3 down")
				return track.ID()
			},
			wantErr:       domain.ErrAudioTemporarilyUnavailable,
			wantStatus:    ptrStatus(domain.AcquisitionReady),
			wantScheduled: false,
		},
		{
			name: "pending track is not streamable",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return track.ID()
			},
			wantErr:       domain.ErrAudioNotAvailable,
			wantStatus:    ptrStatus(domain.AcquisitionPending),
			wantScheduled: false,
		},
		{
			name: "track not found",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				return librarytest.NewTrackId()
			},
			wantErr:       domain.ErrTrackNotFound,
			wantScheduled: false,
		},
		{
			name: "repo GetByID error propagates",
			setup: func(trRepo *memory.TrackRepo, store *memory.AudioStore) domain.TrackId {
				trRepo.ErrOnGetBy = errRepo
				return librarytest.NewTrackId()
			},
			wantErr:       errRepo,
			wantScheduled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trRepo := memory.NewTrackRepo()
			store := memory.NewAudioStore()
			sched := &memory.Scheduler{}
			trackId := tt.setup(trRepo, store)
			svc := service.NewStreamTrackService(trRepo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

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
				if track.DownloadStatus() != *tt.wantStatus {
					t.Errorf("AcquisitionStatus = %v, want %v", track.DownloadStatus(), *tt.wantStatus)
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

	repo := memory.NewTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := memory.NewAudioStore()
	store.ErrOnStream = errors.New("not found")
	sched := &memory.Scheduler{}
	metrics := &memory.Metrics{}
	svc := service.NewStreamTrackService(repo, store, sched, metrics, service.AlwaysEnabled, clock.Now)

	if _, err := svc.Execute(ctx, userId, track.ID()); !errors.Is(err, domain.ErrAudioNotAvailable) {
		t.Fatalf("error = %v, want ErrAudioNotAvailable", err)
	}
	if metrics.StreamRecoveries != 1 {
		t.Errorf("stream-recovery metric = %d, want 1 when a missing object triggers recovery", metrics.StreamRecoveries)
	}
}

func TestStreamTrackService_NoRecoveryMetricOnHealthyStream(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	repo := memory.NewTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := memory.NewAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &memory.Metrics{}
	svc := service.NewStreamTrackService(repo, store, ports.NoopAcquisitionScheduler(), metrics, service.AlwaysEnabled, clock.Now)

	if _, err := svc.Execute(ctx, userId, track.ID()); err != nil {
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
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		sched := &memory.Scheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID(), userId)
		if got.DownloadStatus() != domain.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.DownloadStatus())
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 scheduled re-acquisition, got %d", len(sched.TrackIds))
		}
	})

	t.Run("present file is a no-op", func(t *testing.T) {
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		store.Seed("audio/here.opus", []byte("data"))
		sched := &memory.Scheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/here.opus")
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID(), userId)
		if got.DownloadStatus() != domain.AcquisitionReady {
			t.Errorf("status = %v, want ready (unchanged)", got.DownloadStatus())
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a present file, got %d", len(sched.TrackIds))
		}
	})

	t.Run("missing or foreign track is not found", func(t *testing.T) {
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		sched := &memory.Scheduler{}
		other := librarytest.OtherUserId()
		foreign := librarytest.SeedReadyTrack(t, repo, other, "Theirs", "Artist", "Album", "audio/theirs.opus")
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		for _, id := range []domain.TrackId{librarytest.NewTrackId(), foreign.ID()} {
			if err := svc.RecoverIfMissing(ctx, userId, id); !errors.Is(err, domain.ErrTrackNotFound) {
				t.Fatalf("error = %v, want ErrTrackNotFound", err)
			}
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a not-found track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("non-streamable track is a no-op", func(t *testing.T) {
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		sched := &memory.Scheduler{}
		track := librarytest.SeedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling for a non-streamable track, got %d", len(sched.TrackIds))
		}
	})

	t.Run("exists-check error propagates", func(t *testing.T) {
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		store.ErrOnExists = errors.New("storage down")
		sched := &memory.Scheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/err.opus")
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID()); err == nil {
			t.Fatal("expected an error")
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling on exists error, got %d", len(sched.TrackIds))
		}
	})

	t.Run("persist failure is returned and does not schedule", func(t *testing.T) {
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		sched := &memory.Scheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		errUpdate := errors.New("db down")
		repo.ErrOnUpdate = errUpdate
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		err := svc.RecoverIfMissing(ctx, userId, track.ID())
		if !errors.Is(err, errUpdate) {
			t.Fatalf("error = %v, want wrapping %v", err, errUpdate)
		}
		if len(sched.TrackIds) != 0 {
			t.Errorf("expected no scheduling when the persist failed, got %d", len(sched.TrackIds))
		}
	})

	t.Run("scheduler refusal is logged, not returned", func(t *testing.T) {
		repo := memory.NewTrackRepo()
		store := memory.NewAudioStore()
		sched := &memory.Scheduler{Err: errors.New("queue full")}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := service.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		if err := svc.RecoverIfMissing(ctx, userId, track.ID()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := repo.GetByID(ctx, track.ID(), userId)
		if got.DownloadStatus() != domain.AcquisitionFailed {
			t.Errorf("status = %v, want failed", got.DownloadStatus())
		}
		if len(sched.TrackIds) != 1 {
			t.Errorf("expected 1 schedule attempt, got %d", len(sched.TrackIds))
		}
	})
}

func TestStreamTrackService_ReacquireBoundedByTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		userId := librarytest.UserId()
		repo := memory.NewTrackRepo()
		sched := &memory.StuckScheduler{}
		track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
		svc := service.NewStreamTrackService(repo, memory.NewAudioStore(), sched, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)

		start := time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		_ = svc.RecoverIfMissing(ctx, userId, track.ID())
		memory.AssertScheduleDeadline(t, sched, start, addTrackScheduleBound, clock.Now)
	})
}

func ptrStatus(s domain.DownloadStatus) *domain.DownloadStatus {
	return &s
}

func TestNewStreamTrackService_RefusesAMissingCollaborator(t *testing.T) {
	repo := memory.NewTrackRepo()
	store := memory.NewAudioStore()
	for name, build := range map[string]func(){
		"no scheduler": func() {
			service.NewStreamTrackService(repo, store, nil, &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)
		},
		"no metrics": func() {
			service.NewStreamTrackService(repo, store, ports.NoopAcquisitionScheduler(), nil, service.AlwaysEnabled, clock.Now)
		},
		"no recovery switch": func() {
			service.NewStreamTrackService(repo, store, ports.NoopAcquisitionScheduler(), &librarytest.Metrics{}, nil, clock.Now)
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
	service.NewStreamTrackService(memory.NewTrackRepo(), memory.NewAudioStore(), &memory.Scheduler{}, &librarytest.Metrics{}, service.AlwaysEnabled, nil)
}
