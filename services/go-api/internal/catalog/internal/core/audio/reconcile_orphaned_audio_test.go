package audio_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

const orphanRef = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/album/title.mp3"

type orphanFixture struct {
	repo    *catalog.InMemoryTrackRepo
	store   *catalog.InMemoryAudioStore
	queue   *catalog.InMemoryOrphanedAudioQueue
	metrics *catalog.InMemoryAudioStoreMetrics
	del     *catalog.DeleteTrackService
	sweep   *catalog.ReconcileOrphanedAudioService
}

func newOrphanFixture(opts ...func(*catalog.ReconcileOrphanedAudioService)) *orphanFixture {
	repo := catalog.NewInMemoryTrackRepo()
	store := catalog.NewInMemoryAudioStore()
	queue := catalog.NewInMemoryOrphanedAudioQueue(repo)
	metrics := &catalog.InMemoryAudioStoreMetrics{}
	return &orphanFixture{
		repo:    repo,
		store:   store,
		queue:   queue,
		metrics: metrics,
		del:     catalog.NewDeleteTrackService(repo, store, catalog.WithDeleteTrackOrphanQueue(queue)),
		sweep:   catalog.NewReconcileOrphanedAudioService(queue, store, append(opts, catalog.WithReconcileMetrics(metrics))...),
	}
}

func (f *orphanFixture) orphan(t *testing.T, ref string) {
	t.Helper()
	track := catalog.SeedReadyTrack(t, f.repo, catalog.FixtureUserId(), "Title", "Artist", "Album", ref)
	f.store.Seed(ref, []byte("audio"))
	f.store.ErrOnDelete = errors.New("storage unavailable")
	if err := f.del.Execute(context.Background(), catalog.FixtureUserId(), track.ID); !errors.Is(err, catalog.ErrAudioOrphaned) {
		t.Fatalf("delete err = %v, want ErrAudioOrphaned", err)
	}
	f.store.ErrOnDelete = nil
}

func (f *orphanFixture) run(t *testing.T) catalog.OrphanedAudioSweep {
	t.Helper()
	sweep, err := f.sweep.Execute(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return sweep
}

func TestReconcile_SupersededAudioInsideTheGraceIsKeptAndReusedAudioIsReleasedAfter(t *testing.T) {
	f := newOrphanFixture()
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	f.store.Seed(orphanRef, []byte("audio"))
	held := catalog.OrphanedAudio{AudioRef: orphanRef, UserId: userId, TrackId: catalog.NewTrackId(), NotBefore: time.Now().Add(time.Hour)}
	if err := f.queue.RecordOrphanedAudio(ctx, held); err != nil {
		t.Fatal(err)
	}

	if sweep := f.run(t); sweep != (catalog.OrphanedAudioSweep{}) {
		t.Errorf("sweep inside the grace = %+v, want nothing touched", sweep)
	}
	if _, ok := f.store.Files[orphanRef]; !ok {
		t.Fatal("object deleted inside the grace window")
	}

	catalog.SeedReadyTrack(t, f.repo, userId, "Title", "Artist", "Album", orphanRef)
	held.NotBefore = time.Now().Add(-time.Minute)
	if err := f.queue.RecordOrphanedAudio(ctx, held); err != nil {
		t.Fatal(err)
	}

	if sweep := f.run(t); sweep.Released != 1 || sweep.Deleted != 0 {
		t.Errorf("sweep after the grace = %+v, want the reused ref released, not deleted", sweep)
	}
	f.assertReferencedAudioIntact(t)
}

func (f *orphanFixture) assertReferencedAudioIntact(t *testing.T) {
	t.Helper()
	for _, track := range f.repo.Tracks {
		if track.AudioRef == nil {
			continue
		}
		if _, ok := f.store.Files[*track.AudioRef]; !ok {
			t.Errorf("sweep deleted %q, still referenced by track %s", *track.AudioRef, track.ID)
		}
	}
}

func TestDeleteTrack_OrphanedAudioIsRetriedUntilCleanedUp(t *testing.T) {
	f := newOrphanFixture()
	f.orphan(t, orphanRef)

	recorded, ok := f.queue.Orphans[orphanRef]
	if !ok {
		t.Fatalf("storage delete failure left no durable orphan record; queue = %v", f.queue.Orphans)
	}
	if recorded.UserId != catalog.FixtureUserId() {
		t.Errorf("recorded owner = %v, want %v", recorded.UserId, catalog.FixtureUserId())
	}

	f.store.ErrOnDelete = errors.New("still unavailable")
	if got := f.run(t); got.Failed != 1 {
		t.Fatalf("first sweep = %+v, want one failed retry", got)
	}
	if f.queue.Orphans[orphanRef] == nil || f.queue.Orphans[orphanRef].Attempts != 1 {
		t.Fatalf("failed retry must stay queued with its attempt counted; queue = %v", f.queue.Orphans)
	}
	if _, ok := f.store.Files[orphanRef]; !ok {
		t.Fatal("object vanished although storage delete failed")
	}

	f.store.ErrOnDelete = nil
	if got := f.run(t); got.Deleted != 1 {
		t.Fatalf("second sweep = %+v, want the orphan deleted", got)
	}
	if _, ok := f.store.Files[orphanRef]; ok {
		t.Error("orphaned object still in storage after a successful retry")
	}
	if len(f.queue.Orphans) != 0 {
		t.Errorf("resolved orphan still queued: %v", f.queue.Orphans)
	}
}

func TestReconcileOrphanedAudio_NeverDeletesReferencedAudio(t *testing.T) {
	replaceRef := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/album/title.replace-0f8e7d6c-1234-4abc-9def-001122334455.mp3"

	tests := []struct {
		name    string
		arrange func(t *testing.T, f *orphanFixture)
		want    catalog.OrphanedAudioSweep
		present map[string]bool
		queued  map[string]bool
	}{
		{
			name: "sibling track with equivalent metadata shares the key",
			arrange: func(t *testing.T, f *orphanFixture) {
				catalog.SeedReadyTrack(t, f.repo, catalog.FixtureUserId(), "title", "ARTIST", "album", orphanRef)
			},
			want:    catalog.OrphanedAudioSweep{Released: 1},
			present: map[string]bool{orphanRef: true},
			queued:  map[string]bool{orphanRef: false},
		},
		{
			name: "another user's track references the key",
			arrange: func(t *testing.T, f *orphanFixture) {
				catalog.SeedReadyTrack(t, f.repo, catalog.FixtureOtherUserId(), "Title", "Artist", "Album", orphanRef)
			},
			want:    catalog.OrphanedAudioSweep{Released: 1},
			present: map[string]bool{orphanRef: true},
			queued:  map[string]bool{orphanRef: false},
		},
		{
			name: "owner acquisition pending that may store to the key",
			arrange: func(t *testing.T, f *orphanFixture) {
				catalog.SeedTrack(t, f.repo, catalog.FixtureUserId(), "Title", "Artist", "Album")
			},
			want:    catalog.OrphanedAudioSweep{Deferred: 1},
			present: map[string]bool{orphanRef: true},
			queued:  map[string]bool{orphanRef: true},
		},
		{
			name: "orphaned canonical key beside a live replace key",
			arrange: func(t *testing.T, f *orphanFixture) {
				catalog.SeedReadyTrack(t, f.repo, catalog.FixtureUserId(), "Title", "Artist", "Album", replaceRef)
				f.store.Seed(replaceRef, []byte("replacement"))
			},
			want:    catalog.OrphanedAudioSweep{Deleted: 1},
			present: map[string]bool{orphanRef: false, replaceRef: true},
			queued:  map[string]bool{orphanRef: false},
		},
		{
			name: "orphaned replace key beside the live canonical key",
			arrange: func(t *testing.T, f *orphanFixture) {
				delete(f.queue.Orphans, orphanRef)
				catalog.SeedReadyTrack(t, f.repo, catalog.FixtureUserId(), "Title", "Artist", "Album", orphanRef)
				f.store.Seed(orphanRef, []byte("audio"))
				f.store.Seed(replaceRef, []byte("staged"))
				if err := f.queue.RecordOrphanedAudio(context.Background(), catalog.OrphanedAudio{AudioRef: replaceRef, UserId: catalog.FixtureUserId()}); err != nil {
					t.Fatal(err)
				}
			},
			want:    catalog.OrphanedAudioSweep{Deleted: 1},
			present: map[string]bool{orphanRef: true, replaceRef: false},
			queued:  map[string]bool{replaceRef: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newOrphanFixture()
			f.orphan(t, orphanRef)
			tt.arrange(t, f)

			if got := f.run(t); got != tt.want {
				t.Errorf("sweep = %+v, want %+v", got, tt.want)
			}
			f.assertReferencedAudioIntact(t)
			for ref, want := range tt.present {
				if _, ok := f.store.Files[ref]; ok != want {
					t.Errorf("object %q present = %v, want %v", ref, ok, want)
				}
			}
			for ref, want := range tt.queued {
				if _, ok := f.queue.Orphans[ref]; ok != want {
					t.Errorf("orphan %q queued = %v, want %v", ref, ok, want)
				}
			}
		})
	}
}

func TestReconcileOrphanedAudio_UsageCheckErrorNeverDeletes(t *testing.T) {
	f := newOrphanFixture()
	f.orphan(t, orphanRef)
	f.queue.ErrOnUsage = errors.New("db down")

	if _, err := f.sweep.Execute(context.Background()); err == nil {
		t.Fatal("sweep err = nil, want the usage-check failure surfaced")
	}
	if _, ok := f.store.Files[orphanRef]; !ok {
		t.Error("sweep deleted an object whose reference check failed")
	}
	if _, ok := f.queue.Orphans[orphanRef]; !ok {
		t.Error("orphan dropped from the queue although it was never cleaned up")
	}
}

func TestReconcileOrphanedAudio_AlreadyAbsentObjectResolves(t *testing.T) {
	f := newOrphanFixture()
	f.orphan(t, orphanRef)
	delete(f.store.Files, orphanRef)

	if got := f.run(t); got.Deleted != 1 {
		t.Errorf("sweep = %+v, want the absent object resolved", got)
	}
	if len(f.queue.Orphans) != 0 {
		t.Errorf("absent object still queued: %v", f.queue.Orphans)
	}
}

func TestOrphanedAudio_DegradesWithoutMigration(t *testing.T) {
	unavailable := fmt.Errorf("record: %w", catalog.ErrOrphanedAudioQueueUnavailable)
	f := newOrphanFixture()
	f.queue.ErrOnRecord = unavailable
	f.queue.ErrOnList = unavailable

	f.orphan(t, orphanRef)

	if got, err := f.sweep.Execute(context.Background()); err != nil || got != (catalog.OrphanedAudioSweep{}) {
		t.Errorf("sweep = %+v, %v; want an idle, successful run", got, err)
	}
	if _, ok := f.store.Files[orphanRef]; !ok {
		t.Error("object deleted although nothing was queued")
	}
}

func TestReconcileOrphanedAudio_ListErrorFailsRun(t *testing.T) {
	f := newOrphanFixture()
	f.queue.ErrOnList = errors.New("db down")
	if _, err := f.sweep.Execute(context.Background()); err == nil {
		t.Error("sweep err = nil, want the list failure surfaced")
	}
}

func TestReconcileOrphanedAudio_FailedDeleteNamesTheStuckOrphan(t *testing.T) {
	logs := catalog.CaptureAuditLogs(t)
	f := newOrphanFixture()
	f.orphan(t, orphanRef)
	f.store.ErrOnDelete = errors.New("storage unavailable")

	if got := f.run(t); got.Failed != 1 {
		t.Fatalf("sweep = %+v, want one failed delete", got)
	}

	catalog.AssertAttrs(t, logs.Find(t, "catalog.orphaned_audio_delete_failed"), map[string]string{
		"audio_ref": orphanRef,
		"user_id":   catalog.FixtureUserId().String(),
		"attempts":  "0",
		"error":     "storage unavailable",
	})
	if f.metrics.OrphanedAudioReconcileFailures != 1 {
		t.Errorf("reconcile-failure metric = %d, want 1 so a stuck orphan is alertable",
			f.metrics.OrphanedAudioReconcileFailures)
	}
}

func TestReconcileOrphanedAudio_SwitchOffDeletesNothing(t *testing.T) {
	f := newOrphanFixture(catalog.WithReconcileSwitch(func() bool { return false }))
	f.orphan(t, orphanRef)
	f.store.ErrOnDelete = errors.New("delete must not be attempted")

	if got := f.run(t); got != (catalog.OrphanedAudioSweep{}) {
		t.Errorf("sweep = %+v, want an idle run while the switch is off", got)
	}
	if _, ok := f.store.Files[orphanRef]; !ok {
		t.Error("switched-off sweep deleted the object anyway")
	}
	if _, ok := f.queue.Orphans[orphanRef]; !ok {
		t.Error("switched-off sweep resolved the orphan, so it will never be retried")
	}
}

type usageUnanswerableFor struct {
	*catalog.InMemoryOrphanedAudioQueue
	audioRef string
}

func (q usageUnanswerableFor) AudioUsage(ctx context.Context, audioRef string, owner shared.UserId) (catalog.AudioUsage, error) {
	if audioRef == q.audioRef {
		return catalog.AudioReferenced, errors.New("db down")
	}
	return q.InMemoryOrphanedAudioQueue.AudioUsage(ctx, audioRef, owner)
}

func TestReconcileOrphanedAudio_AbortedRunLogsItsPartialCounts(t *testing.T) {
	const unanswerableRef = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/album/zz-title.mp3"
	logs := catalog.CaptureAuditLogs(t)
	f := newOrphanFixture()
	f.orphan(t, orphanRef)
	f.orphan(t, unanswerableRef)
	f.sweep = catalog.NewReconcileOrphanedAudioService(usageUnanswerableFor{f.queue, unanswerableRef}, f.store)

	if _, err := f.sweep.Execute(context.Background()); err == nil {
		t.Fatal("sweep err = nil, want the usage-check failure surfaced")
	}

	catalog.AssertAttrs(t, logs.Find(t, "catalog.orphaned_audio_reconciled"), map[string]string{
		"deleted": "1",
		"failed":  "0",
	})
}
