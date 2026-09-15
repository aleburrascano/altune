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
}

func NewReconcileOrphanedAudioService(queue ports.OrphanedAudioQueue, audioStore ports.AudioStore) *ReconcileOrphanedAudioService {
	return &ReconcileOrphanedAudioService{queue: queue, audioStore: audioStore}
}

// Execute sweeps one batch. A missing queue table (migration 021 unapplied) is
// not an error: the sweep reports nothing done. It stops early when ctx ends.
func (s *ReconcileOrphanedAudioService) Execute(ctx context.Context) (OrphanedAudioSweep, error) {
	var sweep OrphanedAudioSweep
	orphans, err := s.queue.ListOrphanedAudio(ctx, orphanedAudioBatch)
	if errors.Is(err, ports.ErrOrphanedAudioQueueUnavailable) {
		return sweep, nil
	}
	if err != nil {
		return sweep, fmt.Errorf("reconcile orphaned audio: %w", err)
	}
	for _, orphan := range orphans {
		if err := ctx.Err(); err != nil {
			return sweep, fmt.Errorf("reconcile orphaned audio: %w", err)
		}
		if err := s.reconcile(ctx, orphan, &sweep); err != nil {
			return sweep, fmt.Errorf("reconcile orphaned audio %q: %w", orphan.AudioRef, err)
		}
	}
	logSweep(ctx, sweep)
	return sweep, nil
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
		return s.deleteOrphan(ctx, orphan.AudioRef, sweep)
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

func (s *ReconcileOrphanedAudioService) deleteOrphan(ctx context.Context, audioRef string, sweep *OrphanedAudioSweep) error {
	if err := s.deleteObject(ctx, audioRef); err != nil {
		sweep.Failed++
		return s.queue.MarkOrphanedAudioAttempt(ctx, audioRef, err.Error())
	}
	sweep.Deleted++
	return s.queue.ResolveOrphanedAudio(ctx, audioRef)
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

func logSweep(ctx context.Context, sweep OrphanedAudioSweep) {
	if sweep == (OrphanedAudioSweep{}) {
		return
	}
	slog.InfoContext(ctx, "catalog.orphaned_audio_reconciled",
		"deleted", sweep.Deleted, "released", sweep.Released,
		"deferred", sweep.Deferred, "failed", sweep.Failed)
}
