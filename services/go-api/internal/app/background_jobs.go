package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/playback"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func (a *App) startSimpleJob(
	name jobs.Name,
	interval time.Duration,
	run func(context.Context) error,
	startedAttrs ...any,
) {
	a.startLoggedJob(context.Background(), name, interval, func(ctx context.Context) error {
		if err := run(ctx); err != nil {
			slog.WarnContext(ctx, string(name)+" failed", "error", err)
			return err
		}
		return nil
	}, startedAttrs...)
}

func (a *App) startLoggedJob(
	ctx context.Context,
	name jobs.Name,
	interval time.Duration,
	run func(context.Context) error,
	startedAttrs ...any,
) {
	a.jobs.Ticker(name, interval, run)
	slog.InfoContext(ctx, string(name)+" started", startedAttrs...)
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
		return errors.Join(queueErr, eraseDiscoveryRowsOfDeletedIdentities(ctx, discoveryErasers))
	}, "interval", deletedIdentityErasureInterval.String())
}

func (a *App) discoveryDeletedIdentityErasers() []discovery.DeletedIdentityEraser {
	return []discovery.DeletedIdentityEraser{
		discovery.NewPgxSearchHistoryRepository(a.pool),
		discovery.NewPgxFavoritesRepository(a.pool),
		discovery.NewPgxEventStore(a.pool),
	}
}

func eraseDiscoveryRowsOfDeletedIdentities(ctx context.Context, erasers []discovery.DeletedIdentityEraser) error {
	var failures []error
	erased, idle := int64(0), 0
	for _, eraser := range erasers {
		rows, err := eraser.EraseRowsOfDeletedIdentities(ctx)
		if errors.Is(err, discovery.ErrIdentityStoreUnavailable) {
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
		failures = append(failures, fmt.Errorf("discovery erasure sweep idle, every eraser unavailable: %w", discovery.ErrIdentityStoreUnavailable))
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

func (a *App) startCorpusRefresh(ctx context.Context, store discovery.BehavioralLabelStore) {
	if a.cfg.BehavioralCorpusPath == "" {
		return
	}
	builder := discovery.NewCorpusBuilder(store)
	const lookback = 30 * 24 * time.Hour
	a.startLoggedJob(ctx, jobs.BehavioralCorpusRefresh, 24*time.Hour, func(ctx context.Context) error {
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
	a.startLoggedJob(ctx, jobs.DiscographyEventPrune, discographyPruneInterval, func(ctx context.Context) error {
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

const acquisitionPruneInterval = 24 * time.Hour

type acquisitionRetentionPruner interface {
	Prune(ctx context.Context, now time.Time) (int64, error)
}

func (a *App) startAcquisitionPrune(ctx context.Context, outcomes, rejections acquisitionRetentionPruner) {
	a.startLoggedJob(ctx, jobs.AcquisitionPrune, acquisitionPruneInterval, func(ctx context.Context) error {
		now := time.Now().UTC()
		outcomesPruned, err := outcomes.Prune(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "acquisition outcome prune failed", "error", err)
			return err
		}
		rejectionsPruned, err := rejections.Prune(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "acquisition rejection prune failed", "error", err)
			return err
		}
		if outcomesPruned+rejectionsPruned > 0 {
			slog.InfoContext(ctx, "acquisition rows pruned",
				"outcome_rows", outcomesPruned, "rejection_rows", rejectionsPruned)
		}
		return nil
	}, "interval", acquisitionPruneInterval.String())
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

const (
	unreleasedRefreshInterval = 30 * time.Minute
	unreleasedRefreshPerRun   = 2
)

func (a *App) startUnreleasedRefresh(svc *discovery.UnreleasedService) {
	if svc == nil {
		return
	}
	a.startSimpleJob(jobs.UnreleasedRefresh, unreleasedRefreshInterval, unreleasedRefreshRun(svc), "interval", unreleasedRefreshInterval.String())
}

func unreleasedRefreshRun(svc *discovery.UnreleasedService) func(context.Context) error {
	return func(ctx context.Context) error {
		return svc.RefreshDue(ctx, time.Now().UTC(), unreleasedRefreshPerRun)
	}
}
