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
	a.startTicker(ctx, jobStalePendingReconcile, stalePendingReconcileInterval, func(ctx context.Context) error {
		if _, err := svc.Execute(ctx); err != nil {
			slog.WarnContext(ctx, "stale pending reconcile failed", "error", err)
			return err
		}
		return nil
	})
	slog.Info("stale pending reconcile started", "interval", stalePendingReconcileInterval.String())
}

const orphanedAudioReconcileInterval = 10 * time.Minute

// startOrphanedAudioReconcile retries the storage delete of audio objects a
// partial track delete left behind (recorded by DeleteTrackService), so an
// orphan is cleaned up automatically instead of waiting on an operator. The
// sweep never deletes a key any track still references; before migration 021
// is applied it idles.
func (a *App) startOrphanedAudioReconcile(ctx context.Context, queue catalogPorts.OrphanedAudioQueue, audioStore catalogPorts.AudioStore) {
	if audioStore == nil {
		return
	}
	svc := catalogService.NewReconcileOrphanedAudioService(queue, audioStore)
	a.startTicker(ctx, jobOrphanedAudioReconcile, orphanedAudioReconcileInterval, func(ctx context.Context) error {
		if _, err := svc.Execute(ctx); err != nil {
			slog.WarnContext(ctx, "orphaned audio reconcile failed", "error", err)
			return err
		}
		return nil
	})
	slog.Info("orphaned audio reconcile started", "interval", orphanedAudioReconcileInterval.String())
}

func (a *App) startCorpusRefresh(ctx context.Context, store discoveryPorts.BehavioralLabelStore) {
	if a.cfg.BehavioralCorpusPath == "" {
		return
	}
	builder := eval.NewCorpusBuilder(store)
	const lookback = 30 * 24 * time.Hour
	a.startTicker(ctx, jobBehavioralCorpusRefresh, 24*time.Hour, func(ctx context.Context) error {
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
	a.startTicker(ctx, jobDiscoveryMetricsRollup, 6*time.Hour, func(ctx context.Context) error {
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

// discographyPruneInterval is how often the discography_observed retention prune
// runs. Daily is ample: the retention window is far wider than a day, so nothing
// is urgent to evict, and a missed tick only defers eviction, never skips it.
const discographyPruneInterval = 24 * time.Hour

// startDiscographyPrune schedules the retention prune that keeps discovery_events
// bounded against the discography-quality feature: every discography open appends
// a discography_observed row, so without this the table grows without limit (the
// epic's "bounded window, always" must-hold). The prune evicts only rows older
// than the retention window — always wider than the widest readable aggregate
// window — so it can never remove a case the endpoint could still serve. It is
// leader-only and drained with the other background jobs.
func (a *App) startDiscographyPrune(ctx context.Context, pruner discoveryPorts.DiscographyPruner) {
	a.startTicker(ctx, jobDiscographyEventPrune, discographyPruneInterval, func(ctx context.Context) error {
		pruned, err := pruner.PruneDiscographyObserved(ctx, time.Now().UTC())
		if err != nil {
			slog.WarnContext(ctx, "discography event prune failed", "error", err)
			return err
		}
		if pruned > 0 {
			slog.InfoContext(ctx, "discography events pruned", "rows", pruned)
		}
		return nil
	})
	slog.Info("discography event prune started", "interval", discographyPruneInterval.String())
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
		charts, vocabStore, 50,
	)
	// The service has no loop of its own: the shared ticker drives it, giving it
	// the kill switch, the per-job health signal and the per-tick leadership
	// re-check, and its goroutine is drained with the other background tasks.
	a.startTicker(ctx, jobVocabularyRefresh, vocabRefreshInterval, func(ctx context.Context) error {
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
