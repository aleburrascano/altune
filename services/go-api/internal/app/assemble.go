package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Handles struct {
	Pool      *pgxpool.Pool
	Redis     *goredis.Client
	Transport http.RoundTripper
	LogRing   *logging.RingBuffer
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
	a.dbPing = h.Pool.Ping
	a.redisClient = h.Redis
	a.lifecycleDone = ctx.Done()
}

func (a *App) compose(ctx context.Context, transport http.RoundTripper) (http.Handler, error) {
	verifier, err := a.connectAuth(ctx)
	if err != nil {
		return nil, err
	}
	a.openEventBus()
	if err := a.applyDisabledJobs(ctx); err != nil {
		return nil, fmt.Errorf("startup switches: %w", err)
	}
	f, err := a.wireFeatures(ctx, transport)
	if err != nil {
		return nil, err
	}
	r := a.mountRoutes(verifier, f.catalog, f.playback.handler, f.discovery.Handler)
	a.startAlertMonitor(ctx)
	a.wireObserve(ctx, r, verifier, a.eventTap)
	a.startCatalogJobs(ctx, f.catalog, f.playback)
	a.jobs.Lead(ctx, jobs.NewElection(a.pool))
	return r, nil
}

func (a *App) openEventBus() {
	a.eventBus = events.NewInProcessBus(time.Now, filepath.Join(os.TempDir(), eventIDFloorFile))
	a.eventTap = observe.NewEventTap(a.eventBus)
}

type features struct {
	discovery *discovery.Discovery
	catalog   catalogWiring
	playback  playbackWiring
}

func (a *App) wireFeatures(ctx context.Context, transport http.RoundTripper) (features, error) {
	background, err := runloop.NewTracker(runloop.TrackerLimits{MaxInFlight: a.cfg.BackgroundMaxInFlight})
	if err != nil {
		return features{}, fmt.Errorf("background tracker: %w", err)
	}
	a.background = background
	disc, err := discovery.New(a.discoveryDeps(ctx, transport))
	if err != nil {
		return features{}, fmt.Errorf("discovery: %w", err)
	}
	a.keepSearches(disc)
	a.startDiscoveryBackgroundJobs(ctx, disc)
	cat, err := a.wireCatalog(ctx, a.eventTap, disc)
	if err != nil {
		return features{}, fmt.Errorf("catalog: %w", err)
	}
	a.scheduler = cat.scheduler
	if err := a.applyStartupSwitches(ctx); err != nil {
		return features{}, fmt.Errorf("startup switches: %w", err)
	}
	playback := a.wirePlayback(ctx, cat.trackRepo)
	disc.Handler.WithOwnershipEnrichment(newOwnershipEnrichment(cat, a.background))
	return features{discovery: disc, catalog: cat, playback: playback}, nil
}
