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

func (a *App) startSimpleJob(
	ctx context.Context,
	name jobName,
	interval time.Duration,
	run func(context.Context) error,
	startedAttrs ...any,
) {
	a.startLoggedJob(ctx, name, interval, func(ctx context.Context) error {
		if err := run(ctx); err != nil {
			slog.WarnContext(ctx, string(name)+" failed", "error", err)
			return err
		}
		return nil
	}, startedAttrs...)
}

func (a *App) startLoggedJob(
	ctx context.Context,
	name jobName,
	interval time.Duration,
	run func(context.Context) error,
	startedAttrs ...any,
) {
	a.startTicker(ctx, name, interval, run)
	slog.Info(string(name)+" started", startedAttrs...)
}

const stalePendingReconcileInterval = 10 * time.Minute

func (a *App) startStalePendingReconcile(ctx context.Context, repo catalogPorts.StalePendingFailer) {
	svc := catalogService.NewReconcileStalePendingService(repo)
	a.startSimpleJob(ctx, jobStalePendingReconcile, stalePendingReconcileInterval, func(ctx context.Context) error {
		_, err := svc.Execute(ctx)
		return err
	}, "interval", stalePendingReconcileInterval.String())
}

const orphanedAudioReconcileInterval = 10 * time.Minute

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

const deletedIdentityErasureInterval = time.Hour

func (a *App) startDeletedIdentityErasure(ctx context.Context, svc *playbackService.ForgetDeletedIdentitiesService) {
	discoveryErasers := a.discoveryDeletedIdentityErasers()
	a.startSimpleJob(ctx, jobDeletedIdentityErasure, deletedIdentityErasureInterval, func(ctx context.Context) error {
		_, queueErr := svc.Execute(ctx)
		return errors.Join(queueErr, eraseDiscoveryRowsOfDeletedIdentities(ctx, discoveryErasers))
	}, "interval", deletedIdentityErasureInterval.String())
}

func (a *App) discoveryDeletedIdentityErasers() []discoveryPorts.DeletedIdentityEraser {
	return []discoveryPorts.DeletedIdentityEraser{
		discoveryPersistence.NewPgxSearchHistoryRepository(a.pool),
		discoveryPersistence.NewPgxFavoritesRepository(a.pool),
		discoveryPersistence.NewPgxEventStore(a.pool),
	}
}

func eraseDiscoveryRowsOfDeletedIdentities(ctx context.Context, erasers []discoveryPorts.DeletedIdentityEraser) error {
	var erased int64
	var failures []error
	var idle int
	for _, eraser := range erasers {
		rows, err := eraser.EraseRowsOfDeletedIdentities(ctx)
		if errors.Is(err, discoveryPorts.ErrIdentityStoreUnavailable) {
			slog.WarnContext(ctx, "discovery.deleted_identity_sweep_idle", "error", err)
			idle++
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("erase discovery rows of deleted identities: %w", err))
			continue
		}
		erased += rows
	}
	if idle > 0 && idle == len(erasers) {
		failures = append(failures, fmt.Errorf("discovery erasure sweep idle, every eraser unavailable: %w", discoveryPorts.ErrIdentityStoreUnavailable))
	}
	logDiscoveryErasureSweep(ctx, erased)
	return errors.Join(failures...)
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
	a.startLoggedJob(ctx, jobBehavioralCorpusRefresh, 24*time.Hour, func(ctx context.Context) error {
		since := time.Now().UTC().Add(-lookback)
		if err := builder.Materialize(ctx, since, since.Format("2006-01-02"), a.cfg.BehavioralCorpusPath); err != nil {
			slog.WarnContext(ctx, "behavioral corpus materialize failed", "error", err)
			return err
		}
		slog.InfoContext(ctx, "behavioral corpus materialized", "path", a.cfg.BehavioralCorpusPath)
		return nil
	}, "path", a.cfg.BehavioralCorpusPath)
}

const discographyPruneInterval = 24 * time.Hour

type discoveryEventRetentionPruner interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

func (a *App) startDiscographyPrune(ctx context.Context, pruner discoveryEventRetentionPruner) {
	a.startLoggedJob(ctx, jobDiscographyEventPrune, discographyPruneInterval, func(ctx context.Context) error {
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
	}, "interval", discographyPruneInterval.String())
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
	a.startSimpleJob(ctx, jobVocabularyRefresh, vocabRefreshInterval, func(ctx context.Context) error {
		return a.vocabRefresh.RunOnce(ctx)
	})
}

func (a *App) buildChartProviders(cf clientFactory) []discoveryPorts.ChartProvider {
	var charts []discoveryPorts.ChartProvider
	deezerClient := cf.chart()
	charts = append(charts, providers.NewDeezerAdapter(deezerClient))
	if lfm := buildLastFMAdapter(a.cfg, cf.chart()); lfm != nil {
		charts = append(charts, lfm)
	}
	return charts
}
