package app

import (
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestWireCatalogSchedulerGating pins wireCatalog's observable object graph so
// splitting it into named wiring steps stays behavior-preserving: with no
// acquisition source the scheduler and the retry/reacquire handlers must stay
// nil, and with a source they must all be built and the scheduler recorded on
// the App for shutdown.
func TestWireCatalogSchedulerGating(t *testing.T) {
	tests := []struct {
		name          string
		ytMusic       bool
		wantScheduler bool
	}{
		{name: "no sources leaves acquisition unwired", ytMusic: false, wantScheduler: false},
		{name: "a source wires scheduler and retry handlers", ytMusic: true, wantScheduler: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				cfg: &config.Config{MusicDir: t.TempDir(), YtMusicEnabled: tt.ytMusic},
				sem: make(chan struct{}, 1),
			}

			got, err := a.wireCatalog(nil, nil, nil)
			if err != nil {
				t.Fatalf("wireCatalog: %v", err)
			}

			assertCatalogHandlersBuilt(t, got)
			if (a.scheduler != nil) != tt.wantScheduler {
				t.Errorf("a.scheduler set = %v, want %v", a.scheduler != nil, tt.wantScheduler)
			}
			if (got.retryH != nil) != tt.wantScheduler {
				t.Errorf("retryH set = %v, want %v", got.retryH != nil, tt.wantScheduler)
			}
			if (got.reacquireH != nil) != tt.wantScheduler {
				t.Errorf("reacquireH set = %v, want %v", got.reacquireH != nil, tt.wantScheduler)
			}
		})
	}
}

func TestWireCatalogFailsWithoutAudioStore(t *testing.T) {
	a := &App{cfg: &config.Config{YtMusicEnabled: true}}

	if _, err := a.wireCatalog(nil, nil, nil); err == nil {
		t.Fatal("wireCatalog with no audio store config: want error, got nil")
	}
	if a.scheduler != nil {
		t.Error("scheduler must not be wired when the audio store fails")
	}
}

// TestWireCatalogPrincipalDefault_AdmitsOneUserUpToGlobalDepth proves #2789:
// the production wiring's default (ACQUISITION_PRINCIPAL_QUEUE_DEPTH unset,
// so AcquisitionPrincipalQueueDepth is the zero value) leaves the per-principal
// cap disabled, so one self-hosted user's saves past worker concurrency are
// admitted up to the shared global queue depth instead of being refused with
// ErrPrincipalQueueFull. It builds the scheduler through the real wireCatalog
// path with a saturated worker semaphore, so every admitted job parks on the
// sem instead of running the real acquire pipeline.
func TestWireCatalogPrincipalDefault_AdmitsOneUserUpToGlobalDepth(t *testing.T) {
	const concurrency = 2
	a := &App{
		cfg: &config.Config{
			MusicDir:               t.TempDir(),
			YtMusicEnabled:         true,
			AcquisitionConcurrency: concurrency,
		},
		sem: make(chan struct{}, concurrency),
	}
	// Saturate the workers so every admitted job blocks on the semaphore and
	// keeps holding its slot for the duration of the test.
	for i := 0; i < concurrency; i++ {
		a.sem <- struct{}{}
	}

	if _, err := a.wireCatalog(nil, nil, nil); err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if a.scheduler == nil {
		t.Fatal("scheduler must be wired with a source configured")
	}
	t.Cleanup(func() { a.scheduler.Shutdown(context.Background()) })

	userA := shared.NewUserId(uuid.New())
	const globalDepth = concurrency * 4 // acqService.defaultQueueDepthFactor
	for i := 0; i < globalDepth; i++ {
		if err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user: err = %v, want nil (past concurrency %d, within global depth)", i+1, globalDepth, err, concurrency)
		}
	}

	if err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); !errors.Is(err, acqService.ErrAcquisitionQueueFull) {
		t.Fatalf("schedule past global depth: err = %v, want ErrAcquisitionQueueFull", err)
	}
}

// TestWireCatalogEnforcesPrincipalQueueCap proves #1418: setting
// ACQUISITION_PRINCIPAL_QUEUE_DEPTH (a non-zero AcquisitionPrincipalQueueDepth)
// turns the per-principal fair-share gate on, so one user past its explicit
// share is rejected while global admission slots remain for other users. It
// builds the scheduler through the real wireCatalog path with a saturated
// worker semaphore, so every admitted job parks on the sem (holding its
// principal slot) instead of running the real acquire pipeline.
func TestWireCatalogEnforcesPrincipalQueueCap(t *testing.T) {
	const concurrency = 2
	const principalCap = concurrency // explicit opt-in via ACQUISITION_PRINCIPAL_QUEUE_DEPTH
	a := &App{
		cfg: &config.Config{
			MusicDir:                       t.TempDir(),
			YtMusicEnabled:                 true,
			AcquisitionConcurrency:         concurrency,
			AcquisitionPrincipalQueueDepth: principalCap,
		},
		sem: make(chan struct{}, concurrency),
	}
	// Saturate the workers so every admitted job blocks on the semaphore and
	// keeps holding its per-principal slot for the duration of the test.
	for i := 0; i < concurrency; i++ {
		a.sem <- struct{}{}
	}

	if _, err := a.wireCatalog(nil, nil, nil); err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if a.scheduler == nil {
		t.Fatal("scheduler must be wired with a source configured")
	}
	t.Cleanup(func() { a.scheduler.Shutdown(context.Background()) })

	userA := shared.NewUserId(uuid.New())
	// Fill userA's explicit share, then push two arrivals past it.
	for i := 0; i < principalCap; i++ {
		if err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule within userA share: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), "")
		if !errors.Is(err, acqService.ErrPrincipalQueueFull) {
			t.Fatalf("schedule past userA share: err = %v, want ErrPrincipalQueueFull", err)
		}
	}

	// Global slots remain: a different principal is still admitted.
	if err := a.scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule for second principal while global slots remain: err = %v, want nil", err)
	}
}

func assertCatalogHandlersBuilt(t *testing.T, w catalogWiring) {
	t.Helper()
	if w.trackRepo == nil || w.setTrackNumberSvc == nil {
		t.Error("trackRepo and setTrackNumberSvc must be wired")
	}
	if w.trackHandler == nil || w.libraryHandler == nil || w.playlistHandler == nil {
		t.Error("track, library and playlist handlers must be wired")
	}
	if w.streamHandler == nil || w.audioURLHandler == nil {
		t.Error("stream and audio URL handlers must be wired")
	}
}
