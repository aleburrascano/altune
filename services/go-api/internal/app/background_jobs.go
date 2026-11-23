package app

import (
	"altune/go-api/internal/app/housekeeping"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/playback"
	"context"
	"errors"
	"time"
)

func (a *App) startSimpleJob(name jobs.Name, interval time.Duration, run func(context.Context) error, startedAttrs ...any) {
	a.jobs.StartTicker(context.Background(), name, interval, jobs.WarnOnFailure(name, run), startedAttrs...)
}

const orphanedAudioReconcileInterval = 10 * time.Minute

func (a *App) startOrphanedAudioReconcile(ctx context.Context, queue catalog.OrphanedAudioQueue, audioStore catalog.AudioStore) {
	if audioStore == nil {
		return
	}
	svc := catalog.NewReconcileOrphanedAudioService(queue, audioStore,
		catalog.WithReconcileSwitch(a.jobs.Switch(jobs.OrphanedAudioReconcile)),
		catalog.WithReconcileMetrics(catalog.NewExpvarAudioStoreMetrics()),
	)
	a.startSimpleJob(jobs.OrphanedAudioReconcile, orphanedAudioReconcileInterval, func(ctx context.Context) error {
		_, err := svc.Execute(ctx)
		return err
	}, "interval", orphanedAudioReconcileInterval.String())
}

const deletedIdentityErasureInterval = time.Hour

func (a *App) startDeletedIdentityErasure(ctx context.Context, svc *playback.ErasureSweeper) {
	discoveryErasers := a.discoveryDeletedIdentityErasers()
	a.startSimpleJob(jobs.DeletedIdentityErasure, deletedIdentityErasureInterval, func(ctx context.Context) error {
		_, queueErr := svc.Execute(ctx)
		return errors.Join(queueErr, housekeeping.EraseDiscoveryRowsOfDeletedIdentities(ctx, discoveryErasers))
	}, "interval", deletedIdentityErasureInterval.String())
}

func (a *App) discoveryDeletedIdentityErasers() []discovery.DeletedIdentityEraser {
	return []discovery.DeletedIdentityEraser{
		discovery.NewPgxSearchHistoryRepository(a.pool),
		discovery.NewPgxFavoritesRepository(a.pool),
		discovery.NewPgxEventStore(a.pool),
	}
}

func (a *App) startCorpusRefresh(ctx context.Context, store discovery.BehavioralLabelStore) {
	if a.cfg.BehavioralCorpusPath == "" {
		return
	}
	interval := 24 * time.Hour
	a.jobs.StartTicker(ctx, jobs.BehavioralCorpusRefresh, interval,
		housekeeping.MaterializeBehavioralCorpus(store, a.cfg.BehavioralCorpusPath), "path", a.cfg.BehavioralCorpusPath, "interval", interval.String())
}

const discographyPruneInterval = 24 * time.Hour

func (a *App) startDiscographyPrune(ctx context.Context, pruner housekeeping.DiscoveryEventPruner) {
	a.jobs.StartTicker(ctx, jobs.DiscographyEventPrune, discographyPruneInterval,
		housekeeping.PruneDiscoveryEvents(pruner), "interval", discographyPruneInterval.String())
}

const acquisitionPruneInterval = 24 * time.Hour

func (a *App) startAcquisitionPrune(ctx context.Context, outcomes, rejections housekeeping.RetentionPruner) {
	a.jobs.StartTicker(ctx, jobs.AcquisitionPrune, acquisitionPruneInterval,
		housekeeping.PruneAcquisitionRows(outcomes, rejections), "interval", acquisitionPruneInterval.String())
}

func (a *App) startVocabularyRefresh(vocabStore discovery.VocabularyStore, charts []discovery.ChartProvider) {
	if vocabStore == nil || len(charts) == 0 {
		return
	}
	const vocabRefreshInterval = 6 * time.Hour
	a.vocabRefresh = discovery.NewVocabularyRefreshService(
		charts, vocabStore, 50,
	)
	a.startSimpleJob(jobs.VocabularyRefresh, vocabRefreshInterval, func(ctx context.Context) error {
		return a.vocabRefresh.RunOnce(ctx)
	})
}

const unreleasedRefreshInterval = 30 * time.Minute

func (a *App) startUnreleasedRefresh(svc *discovery.UnreleasedService) {
	if svc == nil {
		return
	}
	a.startSimpleJob(jobs.UnreleasedRefresh, unreleasedRefreshInterval, housekeeping.RefreshDueUnreleased(svc), "interval", unreleasedRefreshInterval.String())
}
