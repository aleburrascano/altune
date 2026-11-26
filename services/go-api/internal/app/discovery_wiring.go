package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/providertransport"
	"altune/go-api/internal/app/searchsettings"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type EvalHandles struct {
	Pool      *pgxpool.Pool
	Transport http.RoundTripper
	Tracker   *background.Tracker
}

func NewEvalDiscovery(ctx context.Context, cfg *config.Config, h EvalHandles) (*search.Discovery, error) {
	transport := h.Transport
	if transport == nil {
		transport = providertransport.Live()
	}
	deps := searchsettings.Deps(ctx, cfg, transport)
	deps.Background = &sync.WaitGroup{}
	deps.Tracker = h.Tracker
	deps.Pool = h.Pool
	return search.New(deps)
}

func (a *App) discoveryDeps(ctx context.Context, transport http.RoundTripper) search.Deps {
	deps := searchsettings.Deps(ctx, a.cfg, transport)
	deps.Background = a.jobs.Background()
	deps.Tracker = a.background
	deps.Pool = a.pool
	deps.Redis = a.redisClient
	deps.ClientEvents = searchsettings.ClientEvents(a.cfg)
	deps.Activity = a.eventTap
	return deps
}

func (a *App) startDiscoveryBackgroundJobs(ctx context.Context, disc *search.Discovery) {
	a.startUnreleasedRefresh(disc.UnreleasedRefresh)
	if a.cfg.BehavioralRankingEnabled {
		a.jobs.EveryInstance(ctx, jobs.BehavioralRankingRefresh, 30*time.Minute,
			jobs.WarnOnFailure(jobs.BehavioralRankingRefresh, disc.Search.RefreshBehavioralScores))
	}
	a.startDiscographyPrune(ctx, disc.Events)
	a.startVocabularyRefresh(disc.SpellingHints, disc.Charts)
}
