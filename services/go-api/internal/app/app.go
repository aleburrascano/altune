package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/sourcecheck"
	"altune/go-api/internal/download"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const eventIDFloorFile = "altune-event-id-floor"

type App struct {
	cfg           *config.Config
	pool          *pgxpool.Pool
	probes        health.Probes
	redisClient   *goredis.Client
	scheduler     *download.BackgroundAcquisitionScheduler
	acquisitionOn bool
	vocabRefresh  *search.SpellingHintsRefreshService
	evalSearch    *search.SearchService
	eventBus      *events.InProcessBus
	eventTap      *monitoring.EventTap
	alertMonitor  *monitoring.AlertMonitor
	logRing       *logging.RingBuffer
	latency       *requestmetrics.Registry
	eventFeed     *monitoring.EventFeed
	evalMeter     *monitoring.EvalMeter
	lifecycleDone <-chan struct{}

	jobs jobs.Runner

	background *background.Tracker
}

func New(cfg *config.Config, logRing *logging.RingBuffer) *App {
	return &App{
		cfg:     cfg,
		logRing: logRing,
		latency: requestmetrics.NewRegistry(),
		jobs:    jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker},
	}
}

func (a *App) connectAuth(ctx context.Context) (auth.TokenVerifier, error) {
	supaVerifier, err := newAuthVerifier(ctx, a.cfg)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	a.probes.Auth = supaVerifier.CheckHealth
	return supaVerifier, nil
}

func (a *App) startCatalogJobs(ctx context.Context, cat catalogWiring, playback playbackWiring) {
	a.startOrphanedAudioReconcile(ctx, cat.orphanedAudio, cat.audioStore)
	a.startDeletedIdentityErasure(ctx, playback.forgetDeletedIdentities)
	a.startSourceCanary(ctx, sourcecheck.Probes{
		YtDlp: cat.ytDlpSearcher, YtDlpAvailable: cat.ytDlpAvailable, InnerTube: cat.ytmCanary,
		YtMusicEnabled: a.cfg.YtMusicEnabled, YtDlpEnabled: a.cfg.YtDLPEnabled,
	})
	a.startAcquisitionPrune(ctx,
		download.NewPostgresOutcomeStore(a.pool),
		download.NewPostgresRejectionStore(a.pool))
	if cat.acquisitionOn {
		a.startAcquisitionUpgrade(ctx, cat.scheduler)
	}
}

func (a *App) applyDisabledJobs(ctx context.Context) error {
	return a.jobs.DisableAtStartup(ctx, a.cfg.DisabledJobs)
}

func (a *App) applyStartupSwitches(ctx context.Context) error {
	if a.cfg.AcquisitionPaused && a.acquisitionOn {
		a.scheduler.Pause()
		slog.InfoContext(ctx, "acquisition started paused")
	}
	if err := a.applyDisabledJobs(ctx); err != nil {
		return err
	}
	return nil
}
