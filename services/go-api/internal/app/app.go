package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/canary"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const eventIDFloorFile = "altune-event-id-floor"

type App struct {
	cfg            *config.Config
	pool           *pgxpool.Pool
	dbPing         func(context.Context) error
	requestTimeout time.Duration
	redisClient    *goredis.Client
	authVerifier   authHealthChecker
	server         *http.Server
	sem            chan struct{}
	scheduler      *acquisition.BackgroundAcquisitionScheduler
	vocabRefresh   *discovery.VocabularyRefreshService
	searchSvc      *discovery.SearchService
	evalSearch     *discovery.SearchService
	eventBus       *events.InProcessBus
	eventTap       *observe.EventTap
	alertMonitor   *observe.AlertMonitor
	logRing        *logging.RingBuffer
	eventFeed      *observe.EventFeed
	evalMeter      *observe.EvalMeter
	lifecycleDone  <-chan struct{}

	jobs jobs.Runner

	fixtureSources *sourceTiers
}

func New(cfg *config.Config, logRing *logging.RingBuffer) *App {
	return &App{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.AcquisitionConcurrency),
		logRing: logRing,
	}
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := a.setup(ctx); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port)
		slog.InfoContext(ctx, "server listening", "addr", addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down")
	}

	shutdown.DrainServer(a.server, shutdown.ServerDrainTimeout)

	slog.InfoContext(ctx, "waiting for background tasks")
	outcomes := a.runShutdownSequence()

	if unstopped := shutdown.Unfinished(outcomes); len(unstopped) > 0 {
		slog.WarnContext(ctx, "closing DB/Redis while components are still shutting down",
			"components", strings.Join(unstopped, ", "))
	}

	a.cleanup(ctx, !shutdown.LeadershipRetained(outcomes))
	slog.InfoContext(ctx, "shutdown complete")
	return nil
}

func (a *App) setup(ctx context.Context) error {
	verifier, err := a.connectInfra(ctx)
	if err != nil {
		return err
	}

	a.eventBus = events.NewInProcessBus(time.Now, filepath.Join(os.TempDir(), eventIDFloorFile))
	a.eventTap = observe.NewEventTap(a.eventBus)

	if err := a.applyDisabledJobs(ctx); err != nil {
		return fmt.Errorf("startup switches: %w", err)
	}

	disc, err := discovery.New(a.discoveryDeps(ctx, newClientFactory(nil)))
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	a.keepSearches(disc)
	a.startDiscoveryBackgroundJobs(ctx, disc)
	cat, err := a.wireCatalog(ctx, a.eventTap, disc)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	a.scheduler = cat.scheduler
	if err := a.applyStartupSwitches(ctx); err != nil {
		return fmt.Errorf("startup switches: %w", err)
	}
	playback := a.wirePlayback(ctx, cat.trackRepo)
	disc.Handler.WithOwnershipEnrichment(newOwnershipEnrichment(cat))

	r := a.mountRoutes(verifier, cat, playback.handler, disc.Handler)
	a.startAlertMonitor(ctx)
	a.wireObserve(ctx, r, verifier, a.eventTap)
	a.startCatalogJobs(ctx, cat, playback)
	a.jobs.Lead(ctx, jobs.NewElection(a.pool))

	a.server = a.newServer(ctx, r)
	return nil
}

func (a *App) connectDatastores(ctx context.Context) error {
	var err error
	a.pool, err = database.NewPool(ctx, a.cfg.DatabaseURL, a.cfg.DBPoolMaxConns)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	a.dbPing = a.pool.Ping

	a.lifecycleDone = ctx.Done()
	a.redisClient = sharedRedis.NewClient(ctx, a.cfg.RedisURL, a.cfg.RedisPoolSize)
	return nil
}

func (a *App) connectInfra(ctx context.Context) (auth.TokenVerifier, error) {
	if err := a.connectDatastores(ctx); err != nil {
		return nil, err
	}
	return a.connectAuth(ctx)
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

func (a *App) cleanup(ctx context.Context, closePool bool) {
	if !closePool {
		slog.ErrorContext(ctx, "leaving DB pool open so process exit releases the retained leader lock")
	}
	if closePool && a.pool != nil {
		a.pool.Close()
	}
	if a.redisClient != nil {
		if err := a.redisClient.Close(); err != nil {
			slog.ErrorContext(ctx, "redis client close error", "error", err)
		}
	}
}
