package app

import (
	acqPersistence "altune/go-api/internal/acquisition/adapters/persistence"
	"altune/go-api/internal/acquisition/adapters/streamrip"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/adapters/testauth"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/observe/eventtap"
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
	"strings"
	"sync"
	"syscall"
	"time"

	acqService "altune/go-api/internal/acquisition/service"
	observeAlert "altune/go-api/internal/observe/alert"

	discoveryService "altune/go-api/internal/discovery/service"

	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type App struct {
	cfg             *config.Config
	pool            *pgxpool.Pool
	dbHealth        dbHealthChecker
	depProbeTimeout time.Duration
	healthCache     healthCache
	redisClient     *goredis.Client
	authVerifier    authHealthChecker
	server          *http.Server
	wg              sync.WaitGroup
	sem             chan struct{}
	scheduler       *acqService.BackgroundAcquisitionScheduler
	vocabRefresh    *discoveryService.VocabularyRefreshService
	searchSvc       *discoveryService.Service
	eventBus        *events.InProcessBus
	eventTap        *eventtap.Tap
	alertMonitor    *observeAlert.Monitor
	logRing         *logging.RingBuffer
	eventFeed       *eventtap.Feed
	evalMeter       *evalmeter.Meter
	lifecycleDone   <-chan struct{}

	election         electionController
	backgroundStarts []backgroundJob

	jobsMu sync.Mutex
	jobs   map[jobName]*jobControl
}

type electionController interface {
	Start(context.Context)
	Await(context.Context) bool
	LeaderContext(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)
	Shutdown(context.Context)
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
		slog.Info("server listening", "addr", addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	a.drainServer(serverDrainTimeout)

	slog.Info("waiting for background tasks")
	outcomes := a.runShutdownSequence()

	if unstopped := unfinishedShutdowns(outcomes); len(unstopped) > 0 {
		slog.Warn("closing DB/Redis while components are still shutting down",
			"components", strings.Join(unstopped, ", "))
	}

	a.cleanup(!leadershipRetained(outcomes))
	slog.Info("shutdown complete")
	return nil
}

func (a *App) setup(ctx context.Context) error {
	verifier, testAuth, err := a.connectInfra(ctx)
	if err != nil {
		return err
	}

	a.eventBus = events.NewInProcessBus()
	tap := eventtap.New(a.eventBus)
	a.eventTap = tap

	clientTransport, err := providerTransport(a.cfg)
	if err != nil {
		return fmt.Errorf("provider replay: %w", err)
	}

	if err := a.applyDisabledJobs(); err != nil {
		return fmt.Errorf("startup switches: %w", err)
	}

	disc := a.wireDiscovery(ctx, newClientFactory(clientTransport))
	a.searchSvc = disc.searchSvc
	cat, err := a.wireCatalog(tap, disc)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	a.scheduler = cat.scheduler
	if err := a.applyStartupSwitches(); err != nil {
		return fmt.Errorf("startup switches: %w", err)
	}
	playback := a.wirePlayback(cat.trackRepo)
	disc.handler.WithOwnershipEnrichment(newOwnershipEnrichment(cat))

	r := a.mountRoutes(verifier, cat, playback.handler, disc.handler, a.wireFeedback())
	if testAuth != nil {
		mountTestLogin(r, testAuth)
	}
	a.startAlertMonitor(ctx)
	a.wireObserve(ctx, r, verifier, tap)
	a.startCatalogJobs(ctx, cat, playback)
	a.startBackgroundWhenLeader(ctx)

	a.server = a.newServer(ctx, r)
	return nil
}

func (a *App) connectDatastores(ctx context.Context) error {
	var err error
	a.pool, err = database.NewPool(ctx, a.cfg.DatabaseURL, a.cfg.DBPoolMaxConns)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	a.dbHealth = func(ctx context.Context) database.HealthStatus {
		return database.CheckHealth(ctx, a.pool)
	}

	a.lifecycleDone = ctx.Done()
	a.redisClient = sharedRedis.NewClient(ctx, a.cfg.RedisURL, a.cfg.RedisPoolSize)
	return nil
}

func (a *App) connectInfra(ctx context.Context) (auth.TokenVerifier, *testauth.TestAuth, error) {
	if err := a.connectDatastores(ctx); err != nil {
		return nil, nil, err
	}

	supaVerifier, err := newAuthVerifier(ctx, a.cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: %w", err)
	}
	a.authVerifier = supaVerifier

	testAuth, verifier, err := buildTestAuthVerifier(a.cfg, supaVerifier)
	if err != nil {
		return nil, nil, fmt.Errorf("test auth: %w", err)
	}
	return verifier, testAuth, nil
}

func (a *App) startCatalogJobs(ctx context.Context, cat catalogWiring, playback playbackWiring) {
	a.startStalePendingReconcile(ctx, cat.trackRepo)
	a.startOrphanedAudioReconcile(ctx, cat.orphanedAudio, cat.audioStore)
	a.startDeletedIdentityErasure(ctx, playback.forgetDeletedIdentities)
	a.startSourceCanary(ctx, cat.ytDlpSearcher, cat.ytDlpAvailable,
		sourceToggles{ytMusic: a.cfg.YtMusicEnabled, ytDlp: a.cfg.YtDLPEnabled})
	if a.pool != nil {
		a.startAcquisitionPrune(ctx,
			acqPersistence.NewPgxOutcomeStore(a.pool),
			acqPersistence.NewPgxRejectionStore(a.pool))
	}
}

func (a *App) applyDisabledJobs() error {
	for _, raw := range a.cfg.DisabledJobs {
		name := jobName(raw)
		if !isKnownJobName(name) {
			return fmt.Errorf("DISABLED_JOBS: unknown job %q", raw)
		}
		if a.job(name).disabled.CompareAndSwap(false, true) {
			slog.Info("job disabled at startup", "job", raw)
		}
	}
	return nil
}

func (a *App) applyStartupSwitches() error {
	if a.cfg.AcquisitionPaused && a.scheduler != nil {
		a.scheduler.Pause()
		slog.Info("acquisition started paused")
	}
	if err := a.applyDisabledJobs(); err != nil {
		return err
	}
	for _, raw := range a.cfg.StreamripServices {
		service := strings.ToLower(strings.TrimSpace(raw))
		if service != "" && !streamrip.Supported(service) {
			return fmt.Errorf("STREAMRIP_SERVICES: unsupported service %q", raw)
		}
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

func (a *App) cleanup(closePool bool) {
	if !closePool {
		slog.Error("leaving DB pool open so process exit releases the retained leader lock")
	}
	if closePool && a.pool != nil {
		a.pool.Close()
	}
	if a.redisClient != nil {
		if err := a.redisClient.Close(); err != nil {
			slog.Error("redis client close error", "error", err)
		}
	}
}
