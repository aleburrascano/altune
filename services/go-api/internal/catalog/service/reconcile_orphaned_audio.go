package service

import (
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// orphanedAudioBatch caps how many orphans one sweep retries.
const orphanedAudioBatch = 100

// OrphanedAudioSweep is the outcome of one reconcile run.
type OrphanedAudioSweep struct {
	Deleted  int // storage object deleted (or already gone) and the orphan resolved
	Released int // key referenced by a track again: resolved without deleting
	Deferred int // owner has a pending acquisition: left queued, nothing deleted
	Failed   int // storage delete failed again: attempt recorded, retried next run
}

// ReconcileOrphanedAudioService retries the storage delete of audio objects left
// behind by a partial track delete (see DeleteTrackService).
//
// Invariant: it never deletes an object while any track references its key.
// Keys are shared by tracks with equivalent metadata, and a fresh acquisition
// for the same metadata writes the same canonical key, so every orphan passes
// the queue's AudioUsage gate immediately before its delete; a gate error aborts
// the run rather than deleting.
type ReconcileOrphanedAudioService struct {
	queue      ports.OrphanedAudioQueue
	audioStore ports.AudioStore
	metrics    ports.AudioStoreMetrics
	// sweepEnabled is the runtime kill switch for the whole sweep. Deleting
	// storage objects is all this service does and the deletes are
	// irreversible, so the switch is checked before the queue is even read:
	// while it reports false no object can be deleted, whatever drives the run.
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

// WithReconcileSwitch gates the sweep behind enabled, checked once per run: a
// flip takes effect from the next run, so a run already in flight can still
// delete the rest of its batch (orphanedAudioBatch objects at most). The sweep
// is enabled by default.
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

// Execute sweeps one batch. It stops early when ctx ends, and the counts it
// reports are logged on every exit, including the aborted ones: a run that
// dies halfway has still deleted objects, and that work must not be invisible.
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
	// Returned after the call, never beside it: `return sweep, s.reconcileEach(…)`
	// leaves the order of the copy and the call that fills it unspecified.
	err = s.reconcileEach(ctx, orphans, &sweep)
	return sweep, err
}

// queuedOrphans reads one batch. A missing queue table (migration 021
// unapplied) is not an error: there is nothing to sweep yet.
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

// reconcile handles one orphan. A returned error is a queue (DB) failure and
// aborts the run; a storage failure is recorded on the orphan instead.
func (s *ReconcileOrphanedAudioService) reconcile(ctx context.Context, orphan ports.OrphanedAudio, sweep *OrphanedAudioSweep) error {
	usage, err := s.queue.AudioUsage(ctx, orphan.AudioRef, orphan.UserId)
	if err != nil {
		return err
	}
	switch usage {
	case ports.AudioUnused:
		return s.deleteOrphan(ctx, orphan, sweep)
	case ports.AudioReferenced:
		// A track serves this key again: it is not an orphan, and that track's
		// own delete owns the object from here.
		sweep.Released++
		return s.queue.ResolveOrphanedAudio(ctx, orphan.AudioRef)
	default:
		// Unknown usage is treated like a pending acquisition: never delete.
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

// logDeleteFailure names the orphan the sweep could not delete. attempts is the
// count before this one, so a line whose attempts keeps climbing is a key the
// store refuses to release, which the aggregate counts alone cannot show.
func logDeleteFailure(ctx context.Context, orphan ports.OrphanedAudio, err error) {
	slog.WarnContext(ctx, "catalog.orphaned_audio_delete_failed",
		"audio_ref", orphan.AudioRef,
		"user_id", orphan.UserId.String(),
		"track_id", orphan.TrackId.String(),
		"attempts", orphan.Attempts,
		"error", err)
}

// deleteObject deletes the object, treating an already-absent object as done so
// a store whose Delete errors on a missing key cannot pin the orphan forever.
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
