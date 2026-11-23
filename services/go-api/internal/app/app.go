package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/canary"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/reqmetrics"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const eventIDFloorFile = "altune-event-id-floor"

type App struct {
	cfg           *config.Config
	pool          *pgxpool.Pool
	dbPing        func(context.Context) error
	redisClient   *goredis.Client
	authVerifier  authHealthChecker
	sem           chan struct{}
	scheduler     *acquisition.BackgroundAcquisitionScheduler
	vocabRefresh  *discovery.VocabularyRefreshService
	searchSvc     *discovery.SearchService
	evalSearch    *discovery.SearchService
	eventBus      *events.InProcessBus
	eventTap      *observe.EventTap
	alertMonitor  *observe.AlertMonitor
	logRing       *logging.RingBuffer
	latency       *reqmetrics.Registry
	eventFeed     *observe.EventFeed
	evalMeter     *observe.EvalMeter
	lifecycleDone <-chan struct{}

	jobs jobs.Runner

	fixtureSources *sourceTiers
}

func New(cfg *config.Config, logRing *logging.RingBuffer) *App {
	return &App{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.AcquisitionConcurrency),
		logRing: logRing,
		latency: reqmetrics.NewRegistry(),
	}
}

func (a *App) connectAuth(ctx context.Context) (auth.TokenVerifier, error) {
	supaVerifier, err := newAuthVerifier(ctx, a.cfg)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	a.authVerifier = supaVerifier
	return supaVerifier, nil
}

func (a *App) startCatalogJobs(ctx context.Context, cat catalogWiring, playback playbackWiring) {
	a.startOrphanedAudioReconcile(ctx, cat.orphanedAudio, cat.audioStore)
	a.startDeletedIdentityErasure(ctx, playback.forgetDeletedIdentities)
	a.startSourceCanary(canary.Probes{
		YtDlp: cat.ytDlpSearcher, YtDlpAvailable: cat.ytDlpAvailable, InnerTube: cat.ytmCanary,
		YtMusicEnabled: a.cfg.YtMusicEnabled, YtDlpEnabled: a.cfg.YtDLPEnabled,
	})
	if a.pool != nil {
		a.startAcquisitionPrune(ctx,
			acquisition.NewPgxOutcomeStore(a.pool),
			acquisition.NewPgxRejectionStore(a.pool))
	}
	a.startAcquisitionUpgrade(ctx, cat.scheduler)
}

func (a *App) applyDisabledJobs(ctx context.Context) error {
	return a.jobs.DisableAtStartup(ctx, a.cfg.DisabledJobs)
}

func (a *App) applyStartupSwitches(ctx context.Context) error {
	if a.cfg.AcquisitionPaused && a.scheduler != nil {
		a.scheduler.Pause()
		slog.InfoContext(ctx, "acquisition started paused")
	}
	if err := a.applyDisabledJobs(ctx); err != nil {
		return err
	}
	return nil
}

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverIdleTimeout       = 120 * time.Second
)

func (a *App) newServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}
