package app

import (
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/shared"
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
	adminAlert "altune/go-api/internal/admin/alert"

	catalogPersistence "altune/go-api/internal/catalog/adapters/persistence"
	catalogDomain "altune/go-api/internal/catalog/domain"
	catalogService "altune/go-api/internal/catalog/service"

	discoveryCatalogBridge "altune/go-api/internal/discovery/adapters/catalogbridge"

	discoveryPorts "altune/go-api/internal/discovery/ports"
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
	redisClient     *goredis.Client
	authVerifier    authHealthChecker
	server          *http.Server
	wg              sync.WaitGroup
	sem             chan struct{}
	scheduler       *acqService.BackgroundAcquisitionScheduler
	vocabRefresh    *discoveryService.VocabularyRefreshService
	searchSvc       *discoveryService.Service
	eventBus        *events.InProcessBus
	alertMonitor    *adminAlert.Monitor
	logRing         *logging.RingBuffer
	eventFeed       *eventtap.Feed
	providerHealth  *providerhealth.Store
	evalMeter       *evalmeter.Meter
	lifecycleDone   <-chan struct{}

	election         electionController
	backgroundStarts []backgroundJob

	jobsMu sync.Mutex
	jobs   map[jobName]*jobControl
}

// electionController is the leader-election surface the app depends on: winning
// leadership, scoping a job's context to the current leadership term (nil/false
// when not leader; canceled once leadership is lost), and releasing the lock on
// shutdown. *leader.Election satisfies it in production; tests substitute a
// fake to simulate a leadership handoff without a live Postgres advisory lock.
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	slog.Info("waiting for background tasks")
	outcomes := a.runShutdownSequence()

	if unstopped := unfinishedShutdowns(outcomes); len(unstopped) > 0 {
		// These components blew past their shutdown budget and are presumed
		// still running. cleanup() is about to close the DB pool and Redis
		// client out from under them, so name them loudly first.
		slog.Warn("closing DB/Redis while components are still shutting down",
			"components", strings.Join(unstopped, ", "))
	}

	a.cleanup(!leadershipRetained(outcomes))
	slog.Info("shutdown complete")
	return nil
}

func (a *App) setup(ctx context.Context) error {
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

	supaVerifier, err := newAuthVerifier(ctx, a.cfg)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	a.authVerifier = supaVerifier

	// In non-prod (config.TestAuthEnabled), verifier accepts a test token OR a
	// real Supabase token, and testAuth is non-nil so POST /test/login is
	// mounted below. In prod both are absent: verifier is the Supabase verifier
	// alone. a.authVerifier stays the Supabase verifier so /admin health probes
	// the real dependency, not the always-healthy local test path.
	testAuth, verifier, err := buildTestAuthVerifier(a.cfg, supaVerifier)
	if err != nil {
		return fmt.Errorf("test auth: %w", err)
	}

	a.eventBus = events.NewInProcessBus()
	tap := eventtap.New(a.eventBus)

	// One client factory for the whole process, so every provider adapter shares
	// the live transport's per-host rate limiters and connection pool.
	clients := newClientFactory(nil)

	disc := a.wireDiscovery(ctx, clients)
	cat, err := a.wireCatalog(tap, disc.featuredBridge, disc.searchSvc)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	playback := a.wirePlayback(cat.trackRepo)
	disc.handler.WithOwnershipEnrichment(discoveryService.NewOwnershipEnrichmentService(
		discoveryCatalogBridge.NewOwnershipReader(catalogOwnedTrackLister{repo: cat.trackRepo}),
		discoveryCatalogBridge.NewTrackNumberWriter(catalogTrackNumberSetter{svc: cat.setTrackNumberSvc}),
	))

	r := a.mountRoutes(verifier, cat, playback.handler, disc.handler, a.wireFeedback())
	// Mount the non-prod test-login route only when the guard built a test
	// verifier; testAuth is nil in prod, so the route never exists there.
	if testAuth != nil {
		mountTestLogin(r, testAuth)
	}
	// The alert monitor is built before admin wiring so its kill switch can be
	// exposed on the operator-only /admin/alerts routes.
	a.startAlertMonitor(ctx)
	a.wireAdmin(ctx, clients, r, verifier, tap, disc.requestStore, disc.searchSvc, disc.artistSvc)

	a.startStalePendingReconcile(ctx, cat.trackRepo)
	a.startOrphanedAudioReconcile(ctx, cat.orphanedAudio, cat.audioStore)
	a.startDeletedIdentityErasure(ctx, playback.forgetDeletedIdentities)
	a.startBackgroundWhenLeader(ctx)

	a.server = a.newServer(ctx, r)

	return nil
}

func (a *App) newServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// cleanup closes the Redis client and, when closePool is set, the DB pool.
// closePool is false while leadership is retained: the election still holds its
// advisory lock on an acquired pooled connection, and pgxpool.Close blocks until
// every acquired connection is returned, so closing would hang shutdown forever.
// Leaving the pool open lets process exit end the session and free the lock.
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

// catalogOwnedTrackLister and catalogTrackNumberSetter sit at the catalog side of
// the ownership seam: they translate catalog's domain types into the discovery
// port types the catalogbridge speaks, so discovery never imports catalog/domain.

type catalogOwnedTrackLister struct {
	repo *catalogPersistence.PgxTrackRepository
}

func (l catalogOwnedTrackLister) ListOwnedTracks(ctx context.Context, userId shared.UserId) ([]discoveryPorts.OwnedTrack, error) {
	refs, err := l.repo.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		return nil, err
	}
	tracks := make([]discoveryPorts.OwnedTrack, 0, len(refs))
	for _, ref := range refs {
		tracks = append(tracks, discoveryPorts.OwnedTrack{
			TrackID:           ref.ID,
			Title:             ref.Title,
			Artist:            ref.Artist,
			AcquisitionStatus: ref.AcquisitionStatus,
			TrackNumber:       ref.TrackNumber,
		})
	}
	return tracks, nil
}

type catalogTrackNumberSetter struct {
	svc *catalogService.SetTrackNumberService
}

func (s catalogTrackNumberSetter) Execute(ctx context.Context, userId shared.UserId, trackId string, trackNumber int) (bool, error) {
	// The parse lives here, on the catalog side, so ParseTrackId stays catalog
	// behavior and discovery hands the id across as a plain string. A malformed
	// persisted id surfaces as an error rather than a silent no-op.
	id, err := catalogDomain.ParseTrackId(trackId)
	if err != nil {
		return false, fmt.Errorf("parse track id: %w", err)
	}
	return s.svc.Execute(ctx, userId, id, trackNumber)
}
