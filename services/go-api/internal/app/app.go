package app

import (
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/providerhealth"
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

	authProviders "altune/go-api/internal/auth/adapters/providers"

	discoveryCatalogBridge "altune/go-api/internal/discovery/adapters/catalogbridge"

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

	election         electionController
	backgroundStarts []backgroundJob

	jobsMu sync.Mutex
	jobs   map[string]*jobControl
}

// electionController is the leader-election surface the app depends on: winning
// leadership, checking whether it still holds the lock, and releasing it on
// shutdown. *leader.Election satisfies it in production; tests substitute a
// fake to simulate a leadership handoff without a live Postgres advisory lock.
type electionController interface {
	Start(context.Context)
	Await(context.Context) bool
	IsLeader() bool
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

// shutdownOutcome records whether one component's bounded shutdown finished
// within its budget. A component that timed out is presumed still running when
// cleanup() closes the DB pool and Redis client, so the distinction must be
// surfaced rather than swallowed. skipped marks a component whose shutdown was
// deliberately never attempted because a prerequisite did not complete.
type shutdownOutcome struct {
	name      string
	completed bool
	skipped   bool
}

// leadershipRetained reports whether the leader-election release was skipped,
// meaning this instance still holds the advisory lock on a pooled connection.
func leadershipRetained(outcomes []shutdownOutcome) bool {
	for _, o := range outcomes {
		if o.name == leaderElectionComponent && o.skipped {
			return true
		}
	}
	return false
}

// unfinishedShutdowns returns the names of components that did not complete
// shutdown within their budget, in declaration order.
func unfinishedShutdowns(outcomes []shutdownOutcome) []string {
	var names []string
	for _, o := range outcomes {
		if !o.completed {
			names = append(names, o.name)
		}
	}
	return names
}

// shutdownComponent runs fn with a bounded context and reports whether it
// returned before the budget elapsed. fn runs on its own goroutine so a
// component that ignores the deadline cannot wedge the whole shutdown sequence;
// a timeout is surfaced as an outcome (and logged) instead of silently falling
// through to cleanup().
func (a *App) shutdownComponent(name string, timeout time.Duration, fn func(context.Context)) shutdownOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	select {
	case <-done:
		return shutdownOutcome{name: name, completed: true}
	case <-ctx.Done():
		slog.Warn("component shutdown exceeded its budget",
			"component", name, "timeout", timeout.String())
		return shutdownOutcome{name: name, completed: false}
	}
}

// componentShutdown is one row of the ordered shutdown table: a named component
// with its own timeout budget and a nil-checked shutdown. Collapsing the
// previously copy-pasted blocks into a table means a newly added shutdownable
// field is a single row that cannot skip the nil-check or the bounded,
// outcome-reporting shutdownComponent path. requires, when set, names an
// earlier row that must have completed for this row to run at all; blockedMsg
// is the error logged when it did not.
type componentShutdown struct {
	name       string
	timeout    time.Duration
	shutdown   func(context.Context)
	requires   string
	blockedMsg string
}

// shutdownPlan is the ordered shutdown table. Every row, the two wait-group
// drains included, runs through the single bounded shutdownComponent path.
// The background drain MUST run before the leader-election lock is released:
// releasing first would let the next instance win leadership and start its own
// copies while these are still mid-flight (e.g. the corpus refresh's blocking
// Materialize), running the same leader-only job twice. Ordering alone is not
// enough: a drain that times out leaves those jobs running, so the release row
// requires the drain to have completed and is skipped otherwise.
func (a *App) shutdownPlan() []componentShutdown {
	return []componentShutdown{
		{name: "alert monitor", timeout: 5 * time.Second, shutdown: func(ctx context.Context) {
			if a.alertMonitor != nil {
				a.alertMonitor.Shutdown(ctx)
			}
		}},
		{name: "event feed", timeout: 5 * time.Second, shutdown: func(ctx context.Context) {
			if a.eventFeed != nil {
				a.eventFeed.Shutdown(ctx)
			}
		}},
		{name: "eval meter", timeout: 5 * time.Second, shutdown: func(ctx context.Context) {
			if a.evalMeter != nil {
				a.evalMeter.Shutdown(ctx)
			}
		}},
		{name: "vocabulary refresh", timeout: 10 * time.Second, shutdown: func(ctx context.Context) {
			if a.vocabRefresh != nil {
				a.vocabRefresh.Shutdown(ctx)
			}
		}},
		{name: "acquisition scheduler", timeout: 30 * time.Second, shutdown: func(ctx context.Context) {
			if a.scheduler != nil {
				a.scheduler.Shutdown(ctx)
			}
		}},
		{name: backgroundTasksComponent, timeout: backgroundDrainTimeout, shutdown: a.waitBackground},
		{
			name: leaderElectionComponent, timeout: 5 * time.Second,
			shutdown: func(ctx context.Context) {
				if a.election != nil {
					a.election.Shutdown(ctx)
				}
			},
			requires: backgroundTasksComponent,
			blockedMsg: "leadership intentionally NOT released: background drain timed out with " +
				"leader-only jobs still running; the advisory lock clears only when this " +
				"instance's DB session ends (process exit)",
		},
		{name: discoverySearchComponent, timeout: backgroundDrainTimeout, shutdown: a.waitSearchBackground},
	}
}

// runShutdownSequence shuts every shutdownPlan component down in strict order
// and collects each outcome.
func (a *App) runShutdownSequence() []shutdownOutcome {
	return a.runShutdownPlan(a.shutdownPlan())
}

// runShutdownPlan runs plan rows in order, gating each on its requires row.
func (a *App) runShutdownPlan(plan []componentShutdown) []shutdownOutcome {
	outcomes := make([]shutdownOutcome, 0, len(plan))
	completed := make(map[string]bool, len(plan))
	for _, c := range plan {
		o := a.runPlannedShutdown(c, completed)
		completed[o.name] = o.completed
		outcomes = append(outcomes, o)
	}
	return outcomes
}

// runPlannedShutdown runs one plan row, or skips it (logging blockedMsg) when
// the row it requires did not complete.
func (a *App) runPlannedShutdown(c componentShutdown, completed map[string]bool) shutdownOutcome {
	if c.requires != "" && !completed[c.requires] {
		slog.Error(c.blockedMsg, "component", c.name, "requires", c.requires)
		return shutdownOutcome{name: c.name, skipped: true}
	}
	return a.shutdownComponent(c.name, c.timeout, c.shutdown)
}

func (a *App) setup(ctx context.Context) error {
	var err error

	a.pool, err = database.NewPool(ctx, a.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	a.dbHealth = func(ctx context.Context) database.HealthStatus {
		return database.CheckHealth(ctx, a.pool)
	}

	a.redisClient = sharedRedis.NewClient(ctx, a.cfg.RedisURL)

	verifier, err := authProviders.NewSupabaseJWTVerifier(
		ctx,
		a.cfg.SupabaseJWTJWKSURL,
		a.cfg.SupabaseProjectURL,
		a.cfg.SupabaseJWTAud,
	)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	a.authVerifier = verifier

	a.eventBus = events.NewInProcessBus()
	tap := eventtap.New(a.eventBus)

	disc := a.wireDiscovery(ctx)
	cat, err := a.wireCatalog(tap, disc.featuredBridge, disc.searchSvc)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	queueHandler := a.wirePlayback(cat.trackRepo)
	disc.handler.
		WithOwnership(discoveryCatalogBridge.NewOwnershipReader(cat.trackRepo)).
		WithTrackNumberFiller(discoveryCatalogBridge.NewTrackNumberWriter(cat.setTrackNumberSvc))

	r := a.mountRoutes(verifier, cat, queueHandler, disc.handler, a.wireFeedback())
	// The alert monitor is built before admin wiring so its kill switch can be
	// exposed on the operator-only /admin/alerts routes.
	a.startAlertMonitor(ctx)
	a.wireAdmin(ctx, r, verifier, tap, disc.requestStore, disc.searchSvc, disc.artistSvc)

	a.startStalePendingReconcile(ctx, cat.trackRepo)
	a.startBackgroundWhenLeader(ctx)

	a.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler: r,
		// Tie every request context to the app lifecycle context so that
		// server.Shutdown cancels long-lived streaming handlers (SSE) instead
		// of blocking on them until the shutdown timeout elapses.
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return nil
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
		a.redisClient.Close()
	}
}
