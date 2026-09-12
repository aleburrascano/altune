package app

import (
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/discovery/service/eval"
	"context"
	"log/slog"
	"time"

	catalogPorts "altune/go-api/internal/catalog/ports"
	catalogService "altune/go-api/internal/catalog/service"

	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

const stalePendingReconcileInterval = 10 * time.Minute

// startStalePendingReconcile sweeps tracks orphaned at pending by an acquisition
// job that died mid-flight, transitioning them to failed so the retry path can
// reclaim them. The ticker runs once on leader acquisition (startup recovery) and
// then on an interval (ongoing sweep).
func (a *App) startStalePendingReconcile(ctx context.Context, repo catalogPorts.StalePendingFailer) {
	svc := catalogService.NewReconcileStalePendingService(repo)
	a.startTicker(ctx, "stale pending reconcile", stalePendingReconcileInterval, func() error {
		if _, err := svc.Execute(ctx); err != nil {
			slog.WarnContext(ctx, "stale pending reconcile failed", "error", err)
			return err
		}
		return nil
	})
	slog.Info("stale pending reconcile started", "interval", stalePendingReconcileInterval.String())
}

func (a *App) startCorpusRefresh(ctx context.Context, store discoveryPorts.BehavioralLabelStore) {
	if a.cfg.BehavioralCorpusPath == "" {
		return
	}
	builder := eval.NewCorpusBuilder(store)
	const lookback = 30 * 24 * time.Hour
	a.startTicker(ctx, "behavioral corpus refresh", 24*time.Hour, func() error {
		since := time.Now().UTC().Add(-lookback)
		if err := builder.Materialize(ctx, since, since.Format("2006-01-02"), a.cfg.BehavioralCorpusPath); err != nil {
			slog.WarnContext(ctx, "behavioral corpus materialize failed", "error", err)
			return err
		}
		slog.InfoContext(ctx, "behavioral corpus materialized", "path", a.cfg.BehavioralCorpusPath)
		return nil
	})
	slog.Info("behavioral corpus refresh started", "path", a.cfg.BehavioralCorpusPath)
}

func (a *App) startMetricsRollup(ctx context.Context, store discoveryPorts.MetricsRollupStore) {
	a.startTicker(ctx, "discovery metrics rollup", 6*time.Hour, func() error {
		now := time.Now().UTC()
		var firstErr error
		for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
			if err := store.RollupDay(ctx, day); err != nil {
				slog.WarnContext(ctx, "discovery metrics rollup failed",
					"day", day.Format("2006-01-02"), "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		return firstErr
	})
	slog.Info("discovery metrics rollup started")
}

func (a *App) startVocabularyRefresh(ctx context.Context, vocabStore discoveryPorts.VocabularyStore) {
	if vocabStore == nil {
		return
	}
	charts := a.buildChartProviders()
	if len(charts) == 0 {
		return
	}
	const vocabRefreshInterval = 6 * time.Hour
	a.vocabRefresh = discoveryService.NewVocabularyRefreshService(
		charts, vocabStore, vocabRefreshInterval, 50,
	)
	// Driven through the shared ticker rather than the service's own loop so it
	// picks up the kill switch, the per-job health signal and the per-tick
	// leadership re-check that the other background jobs already have.
	a.startTicker(ctx, "vocabulary refresh", vocabRefreshInterval, func() error {
		if err := a.vocabRefresh.RunOnce(ctx); err != nil {
			slog.WarnContext(ctx, "vocabulary refresh failed", "error", err)
			return err
		}
		return nil
	})
	slog.Info("vocabulary refresh started")
}

func (a *App) buildChartProviders() []discoveryPorts.ChartProvider {
	var charts []discoveryPorts.ChartProvider
	deezerClient := newChartClient()
	charts = append(charts, providers.NewDeezerAdapter(deezerClient))
	if a.cfg.HasLastFM() {
		lfmClient := newChartClient()
		charts = append(charts, providers.NewLastFmAdapter(
			lfmClient, a.cfg.LastFMAPIKey,
		))
	}
	return charts
}
