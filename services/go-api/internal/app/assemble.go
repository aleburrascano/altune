package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Handles struct {
	Pool      *pgxpool.Pool
	Redis     *goredis.Client
	Transport http.RoundTripper
	Tracker   *background.Tracker
	LogRing   *logging.RingBuffer
}

func NewTracker(cfg *config.Config) (*background.Tracker, error) {
	tracker, err := background.NewTracker(background.TrackerLimits{MaxInFlight: cfg.BackgroundMaxInFlight})
	if err != nil {
		return nil, fmt.Errorf("background tracker: %w", err)
	}
	return tracker, nil
}

type Assembly struct {
	app     *App
	handler http.Handler
}

func Assemble(ctx context.Context, cfg *config.Config, h Handles) (*Assembly, error) {
	if h.Pool == nil {
		return nil, errors.New("assemble: Handles.Pool is required: the leader election, the stores and /health all use it")
	}
	if h.Transport == nil {
		return nil, errors.New("assemble: Handles.Transport is required, so provider calls never fall back to an unguarded transport")
	}
	if h.Tracker == nil {
		return nil, errors.New("assemble: Handles.Tracker is required: it bounds, counts and drains every detached task, and shutdown drains it before the datastores close")
	}
	a := New(cfg, h.LogRing)
	a.attach(ctx, h)
	handler, err := a.compose(ctx, h.Transport)
	if err != nil {
		return nil, err
	}
	return &Assembly{app: a, handler: handler}, nil
}

func (s *Assembly) Handler() http.Handler { return s.handler }

func (s *Assembly) Jobs() *jobs.Runner { return &s.app.jobs }

func (s *Assembly) Shutdown() []shutdown.Outcome { return s.app.runShutdownSequence() }

func (a *App) attach(ctx context.Context, h Handles) {
	a.pool = h.Pool
	a.probes.DB = h.Pool.Ping
	a.redisClient = h.Redis
	if h.Redis != nil {
		a.probes.Redis = func(ctx context.Context) error { return h.Redis.Ping(ctx).Err() }
	}
	a.background = h.Tracker
	a.lifecycleDone = ctx.Done()
}

func (a *App) compose(ctx context.Context, transport http.RoundTripper) (http.Handler, error) {
	verifier, err := a.connectAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.openEventBus(); err != nil {
		return nil, err
	}
	if err := a.applyDisabledJobs(ctx); err != nil {
		return nil, fmt.Errorf("startup switches: %w", err)
	}
	f, err := a.wireFeatures(ctx, transport)
	if err != nil {
		return nil, err
	}
	r := a.mountRoutes(verifier, f.catalog, f.playback.handler, f.discovery.Handler)
	if err := a.startAlertMonitor(ctx); err != nil {
		return nil, err
	}
	if err := a.wireObserve(ctx, r, verifier, a.eventTap); err != nil {
		return nil, err
	}
	a.startCatalogJobs(ctx, f.catalog, f.playback)
	election, err := jobs.NewElection(a.pool)
	if err != nil {
		return nil, fmt.Errorf("background election: %w", err)
	}
	a.jobs.Lead(ctx, election)
	return r, nil
}

func (a *App) openEventBus() error {
	a.eventBus = events.NewInProcessBus(clock.Now, filepath.Join(os.TempDir(), eventIDFloorFile))
	tap, err := monitoring.NewEventTap(a.eventBus, clock.Now)
	if err != nil {
		return fmt.Errorf("event tap: %w", err)
	}
	a.eventTap = tap
	return nil
}

type features struct {
	discovery *search.Discovery
	catalog   catalogWiring
	playback  playbackWiring
}

func (a *App) wireFeatures(ctx context.Context, transport http.RoundTripper) (features, error) {
	disc, err := search.New(a.discoveryDeps(ctx, transport))
	if err != nil {
		return features{}, fmt.Errorf("discovery: %w", err)
	}
	a.keepSearches(disc)
	a.startDiscoveryBackgroundJobs(ctx, disc)
	cat, err := a.wireCatalog(ctx, transport, a.eventTap, disc)
	if err != nil {
		return features{}, fmt.Errorf("catalog: %w", err)
	}
	a.scheduler = cat.scheduler
	a.acquisitionOn = cat.acquisitionOn
	if err := a.applyStartupSwitches(ctx); err != nil {
		return features{}, fmt.Errorf("startup switches: %w", err)
	}
	playback, err := a.wirePlayback(ctx, cat.trackRepo)
	if err != nil {
		return features{}, fmt.Errorf("playback: %w", err)
	}
	ownership, err := newOwnershipEnrichment(cat, a.background)
	if err != nil {
		return features{}, fmt.Errorf("ownership enrichment: %w", err)
	}
	if err := disc.Handler.AttachOwnership(ownership); err != nil {
		return features{}, fmt.Errorf("ownership enrichment: %w", err)
	}
	return features{discovery: disc, catalog: cat, playback: playback}, nil
}
