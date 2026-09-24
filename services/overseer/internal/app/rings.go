package app

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"context"
	"log/slog"
	"sync"
	"time"
)

const restoreBudget = 5 * time.Second

type trackedRing struct {
	bucket string
	name   string
	ring   *core.RingStore
	saved  int
}

type ringJournal struct {
	mu      sync.Mutex
	log     history.Signals
	tracked []trackedRing
}

func (j *ringJournal) restore(ctx context.Context, registry *core.Registry) {
	if j.log == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, restoreBudget)
	defer cancel()
	var tracked []trackedRing
	for _, b := range registry.Buckets() {
		if r, ok := b.(core.Restorable); ok {
			tracked = append(tracked, j.restoreBucket(ctx, b.Meta().ID, r)...)
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.tracked = tracked
}

func (j *ringJournal) restoreBucket(ctx context.Context, bucket string, r core.Restorable) []trackedRing {
	var tracked []trackedRing
	for name, ring := range safeRings(bucket, r) {
		if ring == nil {
			continue
		}
		tracked = append(tracked, trackedRing{bucket: bucket, name: name, ring: ring, saved: j.restoreRing(ctx, bucket, name, ring)})
	}
	return tracked
}

func (j *ringJournal) restoreRing(ctx context.Context, bucket, name string, ring *core.RingStore) int {
	saved, err := j.log.LoadSignals(ctx, bucket, name, ring.Cap())
	if err != nil {
		slog.WarnContext(ctx, "overseer.restore.ring_failed", "bucket", bucket, "ring", name, "error", err)
		return 0
	}
	ring.Restore(saved)
	return ring.Cursor()
}

func safeRings(bucket string, r core.Restorable) (rings map[string]*core.RingStore) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("overseer.start.bucket_panic", "bucket", bucket, "stage", "Rings", "recover", rec)
		}
	}()
	return r.Rings()
}

func (j *ringJournal) persist() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.appendFresh()
}

func (j *ringJournal) flushAndDetach() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.appendFresh()
	j.tracked = nil
}

func (j *ringJournal) appendFresh() {
	for i := range j.tracked {
		t := &j.tracked[i]
		fresh, added := t.ring.AddedSince(t.saved)
		j.log.AppendSignals(t.bucket, t.name, t.ring.Cap(), fresh)
		t.saved = added
	}
}
