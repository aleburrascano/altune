package app

import (
	"altune/go-api/internal/app/discoveryconfig"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/providerhttp"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/runloop"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type EvalHandles struct {
	Pool          *pgxpool.Pool
	Redis         *goredis.Client
	Transport     http.RoundTripper
	IdentityStore discovery.IdentityStore
	Tracker       *runloop.Tracker
}

func NewEvalDiscovery(ctx context.Context, cfg *config.Config, h EvalHandles) (*discovery.Discovery, error) {
	transport := h.Transport
	if transport == nil {
		transport = providerhttp.Live()
	}
	deps := discoveryconfig.Deps(ctx, cfg, transport)
	deps.Background = &sync.WaitGroup{}
	tracker, err := evalTracker(h.Tracker)
	if err != nil {
		return nil, err
	}
	deps.Tracker = tracker
	deps.Pool = h.Pool
	deps.Redis = h.Redis
	deps.IdentityStore = h.IdentityStore
	return discovery.New(deps)
}

func evalTracker(given *runloop.Tracker) (*runloop.Tracker, error) {
	if given != nil {
		return given, nil
	}
	return runloop.NewTracker(runloop.TrackerLimits{MaxInFlight: backgroundMaxInFlight})
}

func (a *App) discoveryDeps(ctx context.Context, transport http.RoundTripper) discovery.Deps {
	deps := discoveryconfig.Deps(ctx, a.cfg, transport)
	deps.Background = a.jobs.Background()
	deps.Tracker = a.background
	deps.Pool = a.pool
	deps.Redis = a.redisClient
	deps.ClientEvents = discoveryconfig.ClientEvents(a.cfg)
	deps.Activity = a.eventTap
	return deps
}

func (a *App) startDiscoveryBackgroundJobs(ctx context.Context, disc *discovery.Discovery) {
	a.startUnreleasedRefresh(disc.UnreleasedRefresh)
	if a.cfg.BehavioralRankingEnabled {
		a.jobs.EveryInstance(ctx, jobs.BehavioralRankingRefresh, 30*time.Minute,
			jobs.WarnOnFailure(jobs.BehavioralRankingRefresh, disc.Search.RefreshBehavioralScores))
	}
	a.startCorpusRefresh(ctx, disc.Events)
	a.startDiscographyPrune(ctx, disc.Events)
	a.startVocabularyRefresh(disc.Vocabulary, disc.Charts)
}
