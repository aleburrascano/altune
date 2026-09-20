package app

import (
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/discovery/service/eval"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	catalogMetrics "altune/go-api/internal/catalog/adapters/metrics"
	catalogPorts "altune/go-api/internal/catalog/ports"
	catalogService "altune/go-api/internal/catalog/service"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"

	playbackService "altune/go-api/internal/playback/service"
)

// startSimpleJob schedules a job whose whole tick is one call that either
// succeeds or fails, warning on the failure and announcing the start. Both
// lines are spelled from the job's wire name ("<name> failed", "<name>
// started") because operator alerting keys on that exact text: renaming a
// jobName now renames its log lines with it, which
// TestStartSimpleJob_LogsTheOldTextForEveryMigratedJob pins.
func (a *App) startSimpleJob(
	ctx context.Context,
	name jobName,
	interval time.Duration,
	run func(context.Context) error,
	startedAttrs ...any,
) {
	a.startTicker(ctx, name, interval, func(ctx context.Context) error {
		if err := run(ctx); err != nil {
			slog.WarnContext(ctx, string(name)+" failed", "error", err)
			return err
		}
		return nil
	})
	slog.Info(string(name)+" started", startedAttrs...)
}

const stalePendingReconcileInterval = 10 * time.Minute

// startStalePendingReconcile sweeps tracks orphaned at pending by an acquisition
// job that died mid-flight, transitioning them to failed so the retry path can
// reclaim them. The ticker runs once on leader acquisition (startup recovery) and
// then on an interval (ongoing sweep).
func (a *App) startStalePendingReconcile(ctx context.Context, repo catalogPorts.StalePendingFailer) {
	svc := catalogService.NewReconcileStalePendingService(repo)
	a.startSimpleJob(ctx, jobStalePendingReconcile, stalePendingReconcileInterval, func(ctx context.Context) error {
		_, err := svc.Execute(ctx)
		return err
	}, "interval", stalePendingReconcileInterval.String())
}

const orphanedAudioReconcileInterval = 10 * time.Minute

// startOrphanedAudioReconcile retries the storage delete of audio objects a
// partial track delete left behind (recorded by DeleteTrackService), so an
// orphan is cleaned up automatically instead of waiting on an operator. The
// sweep never deletes a key any track still references; before migration 021
// is applied it idles.
//
// The sweep carries the job's own kill switch, not only the ticker's: the
// deletes are irreversible, so /admin/jobs disabling this job must stop them
// inside the service that issues them rather than at the scheduler alone.
func (a *App) startOrphanedAudioReconcile(ctx context.Context, queue catalogPorts.OrphanedAudioQueue, audioStore catalogPorts.AudioStore) {
	if audioStore == nil {
		return
	}
	svc := catalogService.NewReconcileOrphanedAudioService(queue, audioStore,
		catalogService.WithReconcileSwitch(a.jobSwitch(jobOrphanedAudioReconcile)),
		catalogService.WithReconcileMetrics(catalogMetrics.NewExpvarAudioStoreMetrics()),
	)
	a.startSimpleJob(ctx, jobOrphanedAudioReconcile, orphanedAudioReconcileInterval, func(ctx context.Context) error {
		_, err := svc.Execute(ctx)
		return err
	}, "interval", orphanedAudioReconcileInterval.String())
}

// deletedIdentityErasureInterval is how often the account-deletion sweep runs.
// Hourly bounds how long a deleted account's PII outlives its identity, at one
// indexed pass over the queue-state table and one over each discovery table per
// hour. discovery_events is the widest of them, which is what keeps the cadence
// at an hour rather than something finer.
const deletedIdentityErasureInterval = time.Hour

// startDeletedIdentityErasure erases what accounts deleted out-of-band in
// Supabase left behind. Supabase deletes an identity without telling this
// service, and neither playback_queue_state nor the discovery tables have a
// cascade to reach, so without this the stored queue of a deleted account (track
// list, natural order, free-text source_id — all PII) is erased only if its
// owner called the self-service route first, with an identity they no longer
// have (#1593), and its discovery search text, favorites and telemetry are never
// erased at all (#2236). Queue erasures run through QueueService.Forget, leaving
// the same audit record as that route. Where the identity store is unreadable (a
// plain Postgres carrying no Supabase auth schema) the sweep idles rather than
// erasing.
//
// The queue and the discovery tables are erased independently and their failures
// joined, so one store being down still erases the other rather than holding a
// deleted account's PII in both until the next tick.
func (a *App) startDeletedIdentityErasure(ctx context.Context, svc *playbackService.ForgetDeletedIdentitiesService) {
	discoveryErasers := a.discoveryDeletedIdentityErasers()
	a.startSimpleJob(ctx, jobDeletedIdentityErasure, deletedIdentityErasureInterval, func(ctx context.Context) error {
		_, queueErr := svc.Execute(ctx)
		return errors.Join(queueErr, eraseDiscoveryRowsOfDeletedIdentities(ctx, discoveryErasers))
	}, "interval", deletedIdentityErasureInterval.String())
}

// discoveryDeletedIdentityErasers is every discovery table that stores rows
// keyed by an account and has no cascade to erase them by. A table added to
// discovery with a user_id belongs in this list, and the sweep is the only thing
// that reads it.
func (a *App) discoveryDeletedIdentityErasers() []discoveryPorts.DeletedIdentityEraser {
	return []discoveryPorts.DeletedIdentityEraser{
		discoveryPersistence.NewPgxSearchHistoryRepository(a.pool),
		discoveryPersistence.NewPgxFavoritesRepository(a.pool),
		discoveryPersistence.NewPgxEventStore(a.pool),
	}
}

// eraseDiscoveryRowsOfDeletedIdentities erases each discovery table in turn,
// stopping at the first failure so the rest is retried next run rather than
// reported as done. Each table's delete is idempotent, so a run that erased two
// tables before failing on the third re-erases nothing when it succeeds.
//
// An identity store this deployment cannot read erases nothing and is not an
// error: the sweep says so once and waits, the same answer the queue-state half
// gives, because "no identity is visible" must never be acted on as "every
// identity was deleted".
func eraseDiscoveryRowsOfDeletedIdentities(ctx context.Context, erasers []discoveryPorts.DeletedIdentityEraser) error {
	var erased int64
	for _, eraser := range erasers {
		rows, err := eraser.EraseRowsOfDeletedIdentities(ctx)
		if errors.Is(err, discoveryPorts.ErrIdentityStoreUnavailable) {
			slog.WarnContext(ctx, "discovery.deleted_identity_sweep_idle", "error", err)
			return nil
		}
		if err != nil {
			return fmt.Errorf("erase discovery rows of deleted identities: %w", err)
		}
		erased += rows
	}
	logDiscoveryErasureSweep(ctx, erased)
	return nil
}

func logDiscoveryErasureSweep(ctx context.Context, erased int64) {
	if erased == 0 {
		return
	}
	slog.InfoContext(ctx, "discovery.deleted_identity_rows_erased", "rows", erased)
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

// discoveryEventRetentionPruner is the slice of the event store this job drives:
// the discography_observed prune (its own wider window) plus the per-type prune of
// every other discovery_events type. Declared here, at the consumer, so scheduling
// the whole-table retention needs no change to the discovery ports.
type discoveryEventRetentionPruner interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

// startDiscographyPrune schedules the retention prune that keeps the whole
// discovery_events table bounded: every discography open appends a
// discography_observed row and every search/behavioral signal appends its own, so
// without this the shared table grows without limit (the epic's "bounded window,
// always" must-hold). Each type is evicted only past its own retention window —
// always wider than the widest window that type is read over — so the prune can
// never remove a row a live read could still serve. It is leader-only and drained
// with the other background jobs. (The job's name predates its widening to the
// full table; a rename would touch the leader registry and wiring, out of scope.)
func (a *App) startDiscographyPrune(ctx context.Context, pruner discoveryEventRetentionPruner) {
	a.startTicker(ctx, jobDiscographyEventPrune, discographyPruneInterval, func(ctx context.Context) error {
		now := time.Now().UTC()
		discographyPruned, err := pruner.PruneDiscographyObserved(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "discography event prune failed", "error", err)
			return err
		}
		otherPruned, err := pruner.PruneEvents(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "discovery event retention prune failed", "error", err)
			return err
		}
		if pruned := discographyPruned + otherPruned; pruned > 0 {
			slog.InfoContext(ctx, "discovery events pruned",
				"discography_rows", discographyPruned, "other_rows", otherPruned)
		}
		return nil
	})
	slog.Info("discography event prune started", "interval", discographyPruneInterval.String())
}

func (a *App) startVocabularyRefresh(ctx context.Context, cf clientFactory, vocabStore discoveryPorts.VocabularyStore) {
	if vocabStore == nil {
		return
	}
	charts := a.buildChartProviders(cf)
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
	a.startSimpleJob(ctx, jobVocabularyRefresh, vocabRefreshInterval, func(ctx context.Context) error {
		return a.vocabRefresh.RunOnce(ctx)
	})
}

func (a *App) buildChartProviders(cf clientFactory) []discoveryPorts.ChartProvider {
	var charts []discoveryPorts.ChartProvider
	deezerClient := cf.chart()
	charts = append(charts, providers.NewDeezerAdapter(deezerClient))
	if a.cfg.HasLastFM() {
		lfmClient := cf.chart()
		charts = append(charts, providers.NewLastFmAdapter(
			lfmClient, a.cfg.LastFMAPIKey,
		))
	}
	return charts
}
