package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/download"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const eventIDFloorFile = "altune-event-id-floor"

type application struct {
	cfg           *config.Config
	pool          *pgxpool.Pool
	probes        health.Probes
	redisClient   *goredis.Client
	download      *download.Module
	vocabRefresh  *search.SpellingHintsRefreshService
	evalSearch    *search.SearchService
	eventBus      *events.InProcessBus
	eventTap      *monitoring.EventTap
	clientEvents  monitoring.ClientEventStore
	alertMonitor  *monitoring.AlertMonitor
	logRing       *logging.RingBuffer
	latency       *requestmetrics.Registry
	metrics       *telemetry.Registry
	eventFeed     *monitoring.EventFeed
	evalMeter     *monitoring.EvalMeter
	lifecycleDone <-chan struct{}

	jobs jobs.Runner

	background *background.Tracker
}

func New(cfg *config.Config, logRing *logging.RingBuffer) *application {
	return &application{
		cfg:     cfg,
		logRing: logRing,
		latency: requestmetrics.New(),
		metrics: telemetry.NewRegistry(),
		jobs:    jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker},
	}
}

func (a *application) connectAuth(ctx context.Context) (auth.TokenVerifier, error) {
	supaVerifier, err := newAuthVerifier(ctx, a.cfg, a.metrics)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	a.probes.Auth = supaVerifier.CheckHealth
	return supaVerifier, nil
}

func (a *application) startCatalogJobs(ctx context.Context, cat catalogWiring, playbackModule *playback.Module) {
	a.startOrphanedAudioReconcile(ctx, cat.orphanedAudio, cat.audioStore)
	a.startDeletedIdentityErasure(ctx, playbackModule)
	a.startSourceCanary(ctx, cat.download)
	outcomes, rejections := cat.download.Retention()
	a.startAcquisitionPrune(ctx, outcomes, rejections)
	a.startAcquisitionUpgrade(ctx, cat.download)
}

func (a *application) applyDisabledJobs(ctx context.Context) error {
	if err := a.jobs.DisableAtStartup(ctx, a.cfg.DisabledJobs); err != nil {
		return fmt.Errorf("disable at startup: %w", err)
	}
	return nil
}

func (a *application) applyStartupSwitches(ctx context.Context) error {
	if a.cfg.AcquisitionPaused && a.download.On() {
		a.download.Scheduler().Pause()
		slog.InfoContext(ctx, "acquisition started paused")
	}
	if err := a.applyDisabledJobs(ctx); err != nil {
		return err
	}
	return nil
}
