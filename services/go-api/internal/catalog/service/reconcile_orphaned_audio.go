package service

import (
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const orphanedAudioBatch = 100

type OrphanedAudioSweep struct {
	Deleted  int
	Released int
	Deferred int
	Failed   int
}

type ReconcileOrphanedAudioService struct {
	queue        ports.OrphanedAudioQueue
	audioStore   ports.AudioStore
	metrics      ports.AudioStoreMetrics
	sweepEnabled func() bool
}

func NewReconcileOrphanedAudioService(
	queue ports.OrphanedAudioQueue,
	audioStore ports.AudioStore,
	opts ...func(*ReconcileOrphanedAudioService),
) *ReconcileOrphanedAudioService {
	s := &ReconcileOrphanedAudioService{
		queue:        queue,
		audioStore:   audioStore,
		metrics:      ports.NoopAudioStoreMetrics(),
		sweepEnabled: func() bool { return true },
	}
	return applyOptions(s, opts)
}

func WithReconcileSwitch(enabled func() bool) func(*ReconcileOrphanedAudioService) {
	return func(s *ReconcileOrphanedAudioService) {
		if enabled != nil {
			s.sweepEnabled = enabled
		}
	}
}

func WithReconcileMetrics(m ports.AudioStoreMetrics) func(*ReconcileOrphanedAudioService) {
	return func(s *ReconcileOrphanedAudioService) {
		if m != nil {
			s.metrics = m
		}
	}
}

func (s *ReconcileOrphanedAudioService) Execute(ctx context.Context) (OrphanedAudioSweep, error) {
	var sweep OrphanedAudioSweep
	if !s.sweepEnabled() {
		slog.WarnContext(ctx, "catalog.orphaned_audio_reconcile_disabled")
		return sweep, nil
	}
	defer logSweep(ctx, &sweep)
	orphans, err := s.queuedOrphans(ctx)
	if err != nil {
		return sweep, err
	}
	err = s.reconcileEach(ctx, orphans, &sweep)
	return sweep, err
}

func (s *ReconcileOrphanedAudioService) queuedOrphans(ctx context.Context) ([]ports.OrphanedAudio, error) {
	orphans, err := s.queue.ListOrphanedAudio(ctx, orphanedAudioBatch)
	if errors.Is(err, ports.ErrOrphanedAudioQueueUnavailable) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reconcile orphaned audio: %w", err)
	}
	return orphans, nil
}

func (s *ReconcileOrphanedAudioService) reconcileEach(ctx context.Context, orphans []ports.OrphanedAudio, sweep *OrphanedAudioSweep) error {
	for _, orphan := range orphans {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("reconcile orphaned audio: %w", err)
		}
		if err := s.reconcile(ctx, orphan, sweep); err != nil {
			return fmt.Errorf("reconcile orphaned audio %q: %w", orphan.AudioRef, err)
		}
	}
	return nil
}

func (s *ReconcileOrphanedAudioService) reconcile(ctx context.Context, orphan ports.OrphanedAudio, sweep *OrphanedAudioSweep) error {
	usage, err := s.queue.AudioUsage(ctx, orphan.AudioRef, orphan.UserId)
	if err != nil {
		return err
	}
	switch usage {
	case ports.AudioUnused:
		return s.deleteOrphan(ctx, orphan, sweep)
	case ports.AudioReferenced:
		sweep.Released++
		return s.queue.ResolveOrphanedAudio(ctx, orphan.AudioRef)
	default:
		sweep.Deferred++
		return s.queue.MarkOrphanedAudioAttempt(ctx, orphan.AudioRef, "deferred: owner has a pending acquisition")
	}
}

func (s *ReconcileOrphanedAudioService) deleteOrphan(ctx context.Context, orphan ports.OrphanedAudio, sweep *OrphanedAudioSweep) error {
	if err := s.deleteObject(ctx, orphan.AudioRef); err != nil {
		sweep.Failed++
		s.metrics.OrphanedAudioReconcileFailed()
		logDeleteFailure(ctx, orphan, err)
		return s.queue.MarkOrphanedAudioAttempt(ctx, orphan.AudioRef, err.Error())
	}
	sweep.Deleted++
	return s.queue.ResolveOrphanedAudio(ctx, orphan.AudioRef)
}

func logDeleteFailure(ctx context.Context, orphan ports.OrphanedAudio, err error) {
	slog.WarnContext(ctx, "catalog.orphaned_audio_delete_failed",
		"audio_ref", orphan.AudioRef,
		"user_id", orphan.UserId.String(),
		"track_id", orphan.TrackId.String(),
		"attempts", orphan.Attempts,
		"error", err)
}

func (s *ReconcileOrphanedAudioService) deleteObject(ctx context.Context, audioRef string) error {
	exists, err := s.audioStore.Exists(ctx, audioRef)
	if err != nil {
		return fmt.Errorf("check audio exists: %w", err)
	}
	if !exists {
		return nil
	}
	return s.audioStore.Delete(ctx, audioRef)
}

func logSweep(ctx context.Context, sweep *OrphanedAudioSweep) {
	if *sweep == (OrphanedAudioSweep{}) {
		return
	}
	slog.InfoContext(ctx, "catalog.orphaned_audio_reconciled",
		"deleted", sweep.Deleted, "released", sweep.Released,
		"deferred", sweep.Deferred, "failed", sweep.Failed)
}
