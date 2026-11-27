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

func (a *application) startSimpleJob(ctx context.Context, name jobs.Name, interval time.Duration, run func(context.Context) error, startedAttrs ...any) {
	a.jobs.StartTicker(ctx, name, interval, jobs.WarnOnFailure(name, run), startedAttrs...)
}

const orphanedAudioReconcileInterval = 10 * time.Minute

func (a *application) startOrphanedAudioReconcile(ctx context.Context, queue library.OrphanedAudioQueue, audioStore library.AudioStore) {
	if audioStore == nil {
		return
	}
	svc := library.NewReconcileOrphanedAudioService(queue, audioStore,
		library.NewAudioStoreMetrics(a.metrics), a.jobs.Switch(jobs.OrphanedAudioReconcile))
	a.startSimpleJob(ctx, jobs.OrphanedAudioReconcile, orphanedAudioReconcileInterval, func(ctx context.Context) error {
		_, err := svc.Execute(ctx)
		return err
	}, "interval", orphanedAudioReconcileInterval.String())
}

const deletedIdentityErasureInterval = time.Hour

func (a *application) startDeletedIdentityErasure(ctx context.Context, svc *playback.DeletedUserDataCleanup) {
	discoveryErasers := a.discoveryDeletedIdentityErasers()
	a.startSimpleJob(ctx, jobs.DeletedUserDataErasure, deletedIdentityErasureInterval, func(ctx context.Context) error {
		_, queueErr := svc.Execute(ctx)
		return errors.Join(queueErr, cleanupjobs.EraseDiscoveryRowsOfDeletedIdentities(ctx, discoveryErasers))
	}, "interval", deletedIdentityErasureInterval.String())
}

func (a *application) discoveryDeletedIdentityErasers() []search.DeletedUserDataEraser {
	return []search.DeletedUserDataEraser{
		search.NewPostgresSearchHistoryRepository(a.pool),
		search.NewPostgresFavoritesRepository(a.pool),
		search.NewPostgresEventStore(a.pool, clock.Now),
	}
}

const discographyPruneInterval = 24 * time.Hour

func (a *application) startDiscographyPrune(ctx context.Context, pruner cleanupjobs.DiscoveryEventPruner) {
	a.jobs.StartTicker(ctx, jobs.DiscographyEventPrune, discographyPruneInterval,
		cleanupjobs.PruneDiscoveryEvents(pruner, clock.Now), "interval", discographyPruneInterval.String())
}

const acquisitionPruneInterval = 24 * time.Hour

func (a *application) startAcquisitionPrune(ctx context.Context, outcomes, rejections cleanupjobs.RetentionPruner) {
	a.jobs.StartTicker(ctx, jobs.AcquisitionPrune, acquisitionPruneInterval,
		cleanupjobs.PruneAcquisitionRows(outcomes, rejections, clock.Now), "interval", acquisitionPruneInterval.String())
}

func (a *application) startVocabularyRefresh(ctx context.Context, vocabStore search.SpellingHintsStore, charts []search.ChartProvider) {
	if vocabStore == nil || len(charts) == 0 {
		return
	}
	const (
		vocabRefreshInterval = 6 * time.Hour
		vocabChartDepth      = 50
	)
	a.vocabRefresh = search.NewVocabularyRefreshService(
		charts, vocabStore, vocabChartDepth,
	)
	a.startSimpleJob(ctx, jobs.SpellingHintsRefresh, vocabRefreshInterval, func(ctx context.Context) error {
		return a.vocabRefresh.RunOnce(ctx)
	})
}

const unreleasedRefreshInterval = 30 * time.Minute

func (a *application) startUnreleasedRefresh(ctx context.Context, svc *search.UnreleasedService) {
	if svc == nil {
		return
	}
	a.startSimpleJob(ctx, jobs.UnreleasedRefresh, unreleasedRefreshInterval, cleanupjobs.RefreshDueUnreleased(svc, clock.Now), "interval", unreleasedRefreshInterval.String())
}
