package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"fmt"
	"testing"
)

const orphanRef = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/album/title.mp3"

// orphanFixture is a library whose audio store fails deletes, with the durable
// orphan queue wired into the delete path and the sweep over the same stores.
type orphanFixture struct {
	repo  *catalogtest.TrackRepo
	store *catalogtest.AudioStore
	queue *catalogtest.OrphanedAudioQueue
	del   *DeleteTrackService
	sweep *ReconcileOrphanedAudioService
}

func newOrphanFixture() *orphanFixture {
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	queue := catalogtest.NewOrphanedAudioQueue(repo)
	return &orphanFixture{
		repo:  repo,
		store: store,
		queue: queue,
		del:   NewDeleteTrackService(repo, store, WithDeleteTrackOrphanQueue(queue)),
		sweep: NewReconcileOrphanedAudioService(queue, store),
	}
}

// orphan deletes a ready track at ref while the storage delete fails, leaving
// ref orphaned, then lets storage deletes succeed again.
func (f *orphanFixture) orphan(t *testing.T, ref string) {
	t.Helper()
	track := seedReadyTrack(t, f.repo, testUserId(), "Title", "Artist", "Album", ref)
	f.store.Seed(ref, []byte("audio"))
	f.store.ErrOnDelete = errors.New("storage unavailable")
	if err := f.del.Execute(context.Background(), testUserId(), track.ID); !errors.Is(err, ErrAudioOrphaned) {
		t.Fatalf("delete err = %v, want ErrAudioOrphaned", err)
	}
	f.store.ErrOnDelete = nil
}

func (f *orphanFixture) run(t *testing.T) OrphanedAudioSweep {
	t.Helper()
	sweep, err := f.sweep.Execute(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return sweep
}

// assertReferencedAudioIntact is the sweep's safety invariant: every object
// that any track still references must survive the sweep.
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

// TestDeleteTrack_OrphanedAudioIsRetriedUntilCleanedUp is the #1058 regression:
// a failed storage delete must leave a durable record that the sweep retries
// until the object is gone, not only a log line.
func TestDeleteTrack_OrphanedAudioIsRetriedUntilCleanedUp(t *testing.T) {
	f := newOrphanFixture()
	f.orphan(t, orphanRef)

	recorded, ok := f.queue.Orphans[orphanRef]
	if !ok {
		t.Fatalf("storage delete failure left no durable orphan record; queue = %v", f.queue.Orphans)
	}
	if recorded.UserId != testUserId() {
		t.Errorf("recorded owner = %v, want %v", recorded.UserId, testUserId())
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

// TestReconcileOrphanedAudio_NeverDeletesReferencedAudio proves the sweep never
// deletes an object any track references: keys are shared between tracks with
// equivalent metadata (and a fresh acquisition rewrites the same canonical key),
// and replace attempts store beside it under ".replace-<uuid>" keys.
func TestReconcileOrphanedAudio_NeverDeletesReferencedAudio(t *testing.T) {
	replaceRef := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/album/title.replace-0f8e7d6c-1234-4abc-9def-001122334455.mp3"

	tests := []struct {
		name    string
		arrange func(t *testing.T, f *orphanFixture)
		want    OrphanedAudioSweep
		present map[string]bool // storage key -> object must still exist
		queued  map[string]bool // storage key -> orphan must still be queued
	}{
		{
			name: "sibling track with equivalent metadata shares the key",
			arrange: func(t *testing.T, f *orphanFixture) {
				seedReadyTrack(t, f.repo, testUserId(), "title", "ARTIST", "album", orphanRef)
			},
			want:    OrphanedAudioSweep{Released: 1},
			present: map[string]bool{orphanRef: true},
			queued:  map[string]bool{orphanRef: false},
		},
		{
			name: "another user's track references the key",
			arrange: func(t *testing.T, f *orphanFixture) {
				seedReadyTrack(t, f.repo, testOtherUserId(), "Title", "Artist", "Album", orphanRef)
			},
			want:    OrphanedAudioSweep{Released: 1},
			present: map[string]bool{orphanRef: true},
			queued:  map[string]bool{orphanRef: false},
		},
		{
			name: "owner acquisition pending that may store to the key",
			arrange: func(t *testing.T, f *orphanFixture) {
				seedTrack(t, f.repo, testUserId(), "Title", "Artist", "Album")
			},
			want:    OrphanedAudioSweep{Deferred: 1},
			present: map[string]bool{orphanRef: true},
			queued:  map[string]bool{orphanRef: true},
		},
		{
			name: "orphaned canonical key beside a live replace key",
			arrange: func(t *testing.T, f *orphanFixture) {
				seedReadyTrack(t, f.repo, testUserId(), "Title", "Artist", "Album", replaceRef)
				f.store.Seed(replaceRef, []byte("replacement"))
			},
			want:    OrphanedAudioSweep{Deleted: 1},
			present: map[string]bool{orphanRef: false, replaceRef: true},
			queued:  map[string]bool{orphanRef: false},
		},
		{
			name: "orphaned replace key beside the live canonical key",
			arrange: func(t *testing.T, f *orphanFixture) {
				delete(f.queue.Orphans, orphanRef)
				seedReadyTrack(t, f.repo, testUserId(), "Title", "Artist", "Album", orphanRef)
				f.store.Seed(orphanRef, []byte("audio"))
				f.store.Seed(replaceRef, []byte("staged"))
				if err := f.queue.RecordOrphanedAudio(context.Background(), ports.OrphanedAudio{AudioRef: replaceRef, UserId: testUserId()}); err != nil {
					t.Fatal(err)
				}
			},
			want:    OrphanedAudioSweep{Deleted: 1},
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

// TestReconcileOrphanedAudio_UsageCheckErrorNeverDeletes proves an unanswerable
// reference check aborts the run instead of deleting on a guess.
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

// TestOrphanedAudio_DegradesWithoutMigration covers deploy-before-migration:
// with no orphaned_audio table the delete keeps its prior outcome and the
// sweep idles without failing the job.
func TestOrphanedAudio_DegradesWithoutMigration(t *testing.T) {
	unavailable := fmt.Errorf("record: %w", ports.ErrOrphanedAudioQueueUnavailable)
	f := newOrphanFixture()
	f.queue.ErrOnRecord = unavailable
	f.queue.ErrOnList = unavailable

	f.orphan(t, orphanRef)

	if got, err := f.sweep.Execute(context.Background()); err != nil || got != (OrphanedAudioSweep{}) {
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
