package app

import (
	"altune/go-api/internal/app/cleanupjobs"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/library"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"time"
)

func (a *App) startSimpleJob(name jobs.Name, interval time.Duration, run func(context.Context) error, startedAttrs ...any) {
	a.jobs.StartTicker(context.Background(), name, interval, jobs.WarnOnFailure(name, run), startedAttrs...)
}

const orphanedAudioReconcileInterval = 10 * time.Minute

func (a *App) startOrphanedAudioReconcile(ctx context.Context, queue library.OrphanedAudioQueue, audioStore library.AudioStore) {
	if audioStore == nil {
		return
	}
	svc := library.NewReconcileOrphanedAudioService(queue, audioStore,
		library.NewExpvarAudioStoreMetrics(), a.jobs.Switch(jobs.OrphanedAudioReconcile))
	a.startSimpleJob(jobs.OrphanedAudioReconcile, orphanedAudioReconcileInterval, func(ctx context.Context) error {
		_, err := svc.Execute(ctx)
		return err
	}, "interval", orphanedAudioReconcileInterval.String())
}

const deletedIdentityErasureInterval = time.Hour

func (a *App) startDeletedIdentityErasure(ctx context.Context, svc *playback.DeletedUserDataCleanup) {
	discoveryErasers := a.discoveryDeletedIdentityErasers()
	a.startSimpleJob(jobs.DeletedUserDataErasure, deletedIdentityErasureInterval, func(ctx context.Context) error {
		_, queueErr := svc.Execute(ctx)
		return errors.Join(queueErr, cleanupjobs.EraseDiscoveryRowsOfDeletedIdentities(ctx, discoveryErasers))
	}, "interval", deletedIdentityErasureInterval.String())
}

func (a *App) discoveryDeletedIdentityErasers() []search.DeletedUserDataEraser {
	return []search.DeletedUserDataEraser{
		search.NewPostgresSearchHistoryRepository(a.pool),
		search.NewPostgresFavoritesRepository(a.pool),
		search.NewPostgresEventStore(a.pool),
	}
}

const discographyPruneInterval = 24 * time.Hour

func (a *App) startDiscographyPrune(ctx context.Context, pruner cleanupjobs.DiscoveryEventPruner) {
	a.jobs.StartTicker(ctx, jobs.DiscographyEventPrune, discographyPruneInterval,
		cleanupjobs.PruneDiscoveryEvents(pruner, clock.Now), "interval", discographyPruneInterval.String())
}

const acquisitionPruneInterval = 24 * time.Hour

func (a *App) startAcquisitionPrune(ctx context.Context, outcomes, rejections cleanupjobs.RetentionPruner) {
	a.jobs.StartTicker(ctx, jobs.AcquisitionPrune, acquisitionPruneInterval,
		cleanupjobs.PruneAcquisitionRows(outcomes, rejections, clock.Now), "interval", acquisitionPruneInterval.String())
}

func (a *App) startVocabularyRefresh(vocabStore search.SpellingHintsStore, charts []search.ChartProvider) {
	if vocabStore == nil || len(charts) == 0 {
		return
	}
	const vocabRefreshInterval = 6 * time.Hour
	a.vocabRefresh = search.NewVocabularyRefreshService(
		charts, vocabStore, 50,
	)
	a.startSimpleJob(jobs.SpellingHintsRefresh, vocabRefreshInterval, func(ctx context.Context) error {
		return a.vocabRefresh.RunOnce(ctx)
	})
}

const unreleasedRefreshInterval = 30 * time.Minute

func (a *App) startUnreleasedRefresh(svc *search.UnreleasedService) {
	if svc == nil {
		return
	}
	a.startSimpleJob(jobs.UnreleasedRefresh, unreleasedRefreshInterval, cleanupjobs.RefreshDueUnreleased(svc, clock.Now), "interval", unreleasedRefreshInterval.String())
}
