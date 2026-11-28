package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/searchsettings"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/config"
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
	deps := searchsettings.Deps(ctx, cfg, h.Transport, metrics)
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

func (a *application) discoveryDeps(ctx context.Context, transport http.RoundTripper) search.Deps {
	deps := searchsettings.Deps(ctx, a.cfg, transport, a.metrics)
	deps.Background = a.jobs.Background()
	deps.Tracker = a.background
	deps.Pool = a.pool
	deps.Redis = a.redisClient
	deps.ClientEvents = searchsettings.ClientEvents(a.cfg)
	deps.Activity = a.eventTap
	return deps
}

const behavioralRankingRefreshInterval = 30 * time.Minute

func (a *application) startDiscoveryBackgroundJobs(ctx context.Context, disc *search.Module) {
	a.startUnreleasedRefresh(ctx, disc.UnreleasedRefresh)
	if a.cfg.BehavioralRankingEnabled {
		a.jobs.EveryInstance(ctx, jobs.BehavioralRankingRefresh, behavioralRankingRefreshInterval,
			jobs.WarnOnFailure(jobs.BehavioralRankingRefresh, disc.Search.RefreshBehavioralScores))
	}
	a.startDiscographyPrune(ctx, disc.Events)
	a.startVocabularyRefresh(ctx, disc.SpellingHints, disc.Charts)
}
