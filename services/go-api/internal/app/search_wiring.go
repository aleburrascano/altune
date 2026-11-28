package app

import (
	"altune/go-api/internal/app/bridges/searchfromlibrary"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type EvalHandles struct {
	Pool      *pgxpool.Pool
	Transport http.RoundTripper
	Tracker   *background.Tracker
	Metrics   *telemetry.Registry
}

func NewEvalDiscovery(ctx context.Context, cfg *config.Config, h EvalHandles) (*search.Module, error) {
	if h.Transport == nil {
		return nil, errors.New("eval discovery: EvalHandles.Transport is required, so provider calls never fall back to an unguarded transport")
	}
	metrics := h.Metrics
	if metrics == nil {
		metrics = telemetry.NewRegistry()
	}
	deps := searchBaseDeps(ctx, cfg, h.Transport, metrics)
	deps.Background = &background.Tracker{}
	deps.Activity = search.NoActivityFeed{}
	deps.ClientEvents = search.NoClientEvents{}
	deps.Tracker = h.Tracker
	deps.Pool = h.Pool
	r0, err := search.New(ctx, deps)
	if err != nil {
		return r0, fmt.Errorf("call failed: %w", err)
	}
	return r0, nil
}

func NewProviderTransport(metrics *telemetry.Registry) (http.RoundTripper, error) {
	live, err := outbound.New(search.ProviderRetryRecorder(metrics))
	if err != nil {
		return nil, fmt.Errorf("provider transport: %w", err)
	}
	return search.NewCountingTransport(live, metrics), nil
}

func (a *application) discoveryDeps(ctx context.Context, transport http.RoundTripper) search.Deps {
	deps := searchBaseDeps(ctx, a.cfg, transport, a.metrics)
	deps.Background = a.jobs.Background()
	deps.Tracker = a.background
	deps.Pool = a.pool
	deps.Redis = a.redisClient
	deps.ClientEvents = clientEventSink(a.cfg)
	deps.Activity = a.eventTap
	return deps
}

const behavioralRankingRefreshInterval = 30 * time.Minute

func (a *application) startDiscoveryBackgroundJobs(ctx context.Context, disc *search.Module) {
	a.startUnreleasedRefresh(ctx, disc)
	if a.cfg.BehavioralRankingEnabled {
		a.jobs.EveryInstance(ctx, jobBehavioralRankingRefresh, behavioralRankingRefreshInterval,
			warnOnJobFailure(jobBehavioralRankingRefresh, disc.Search.RefreshBehavioralScores))
	}
	a.startDiscographyPrune(ctx, disc)
	a.startVocabularyRefresh(ctx, disc.SpellingHints, disc.Charts)
}

type evalRunner func(ctx context.Context) (search.SmokeResult, error)

func (a *application) keepSearches(disc *search.Module) {
	a.evalSearch = disc.RankingOnlySearch()
}

func (a *application) buildEvalRunner() evalRunner {
	evalSvc := a.evalSearch
	return func(ctx context.Context) (search.SmokeResult, error) {
		return search.RunSmokeEval(ctx, evalSvc, ids.SystemUserId())
	}
}

func newOwnershipEnrichment(cat catalogWiring, bg *background.Tracker) (*search.OwnershipEnrichmentService, error) {
	r0, err := search.NewOwnershipEnrichmentService(
		bg,
		search.NewOwnershipReader(searchfromlibrary.NewOwnedTracks(cat.trackRepo)),
		searchfromlibrary.NewTrackNumberFiller(cat.setTrackNumberSvc),
	)
	if err != nil {
		return r0, fmt.Errorf("call failed: %w", err)
	}
	return r0, nil
}
