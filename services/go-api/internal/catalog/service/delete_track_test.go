package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"strings"
	"testing"
)

// sharedAudioRef is one storage object two seeded tracks point at.
const sharedAudioRef = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/abbey road/come together.mp3"

func TestDeleteTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.TrackRepo) domain.TrackId
		wantErr error
	}{
		{
			name: "existing track is deleted",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr: nil,
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				return domain.NewTrackId()
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Track", "Artist", "Album")
				repo.ErrOnDelete = errRepo
				return track.ID
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			trackId := tt.setup(repo)
			svc := NewDeleteTrackService(repo, catalogtest.NewAudioStore())

			err := svc.Execute(ctx, userId, trackId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestDeleteTrack_KeepsAudioSharedWithAnotherTrack is the #2203 regression: the
// storage key comes from path-normalized metadata and the dedup key does not,
// so two rows dedup keeps apart can serve one object, and deleting either must
// leave the object to the one that remains.
func TestDeleteTrack_KeepsAudioSharedWithAnotherTrack(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	userId := testUserId()
	deleted := seedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road", sharedAudioRef)
	kept := seedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road (Remastered)", sharedAudioRef)
	assertCollidesOnOneStorageKey(t, deleted, kept)
	store.Seed(sharedAudioRef, []byte("audio"))

	if err := NewDeleteTrackService(repo, store).Execute(context.Background(), userId, deleted.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, ok := store.Files[sharedAudioRef]; !ok {
		t.Fatalf("audio %q deleted while track %s still references it", sharedAudioRef, kept.ID)
	}
}

func TestDeleteTrack_DeletesAudioOnTheLastReference(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	userId := testUserId()
	only := seedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road", sharedAudioRef)
	store.Seed(sharedAudioRef, []byte("audio"))

	if err := NewDeleteTrackService(repo, store).Execute(context.Background(), userId, only.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, ok := store.Files[sharedAudioRef]; ok {
		t.Fatalf("audio %q kept after its last reference was deleted", sharedAudioRef)
	}
}

// TestDeleteTrack_QueuesTheOrphanWhenTheUsageCheckFails pins the unanswerable
// case: an object that may still be served must not be deleted on a guess, and
// the one that is truly orphaned must still reach the sweep that re-checks it.
func TestDeleteTrack_QueuesTheOrphanWhenTheUsageCheckFails(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	queue := catalogtest.NewOrphanedAudioQueue(repo)
	userId := testUserId()
	track := seedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road", sharedAudioRef)
	store.Seed(sharedAudioRef, []byte("audio"))
	repo.ErrOnGetBy = errors.New("db error")

	err := NewDeleteTrackService(repo, store, WithDeleteTrackOrphanQueue(queue)).
		Execute(context.Background(), userId, track.ID)

	if !errors.Is(err, ErrAudioOrphaned) {
		t.Fatalf("delete err = %v, want ErrAudioOrphaned", err)
	}
	if _, ok := store.Files[sharedAudioRef]; !ok {
		t.Fatalf("audio %q deleted while its usage was unknown", sharedAudioRef)
	}
	if _, ok := queue.Orphans[sharedAudioRef]; !ok {
		t.Fatalf("audio %q left behind without being queued for the sweep", sharedAudioRef)
	}
}

// assertCollidesOnOneStorageKey confirms the collision under test is reachable
// rather than contrived: the two rows survive dedup (their keys differ) yet
// their metadata normalizes to the same storage path segments, which is how
// they come to share one audio object.
func assertCollidesOnOneStorageKey(t *testing.T, a, b *domain.Track) {
	t.Helper()
	if a.DedupKey == b.DedupKey {
		t.Fatalf("both tracks dedup to %q, so only one row would exist", a.DedupKey)
	}
	segments := []struct{ field, a, b string }{
		{"artist", a.Artist, b.Artist},
		{"album", a.Album, b.Album},
		{"title", a.Title, b.Title},
	}
	for _, s := range segments {
		if textnorm.NormalizeForMatch(s.a) != textnorm.NormalizeForMatch(s.b) {
			t.Fatalf("%s %q and %q normalize apart, so the keys would differ", s.field, s.a, s.b)
		}
	}
}

func TestDeleteTrackService_OrphanedDeleteMetric(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := catalogtest.NewAudioStore()
	store.ErrOnDelete = errors.New("s3 down")
	metrics := &catalogtest.Metrics{}
	svc := NewDeleteTrackService(repo, store, WithDeleteTrackMetrics(metrics))

	// The track row is deleted but its audio object is orphaned. This is a
	// partial deletion: it must NOT be reported as success. The error is surfaced
	// as ErrAudioOrphaned and the orphaned-delete counter flags the orphan for
	// reconciliation.
	err := svc.Execute(ctx, userId, track.ID)
	if err == nil {
		t.Fatal("expected ErrAudioOrphaned, got nil (orphan silently swallowed as success)")
	}
	if !errors.Is(err, ErrAudioOrphaned) {
		t.Fatalf("error = %v, want ErrAudioOrphaned", err)
	}
	if metrics.OrphanedDeletes != 1 {
		t.Errorf("orphaned-delete metric = %d, want 1 so operators can alert on orphaning", metrics.OrphanedDeletes)
	}
}

func TestDeleteTrackService_NoOrphanMetricOnCleanDelete(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := catalogtest.NewAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &catalogtest.Metrics{}
	svc := NewDeleteTrackService(repo, store, WithDeleteTrackMetrics(metrics))

	if err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.OrphanedDeletes != 0 {
		t.Errorf("orphaned-delete metric = %d, want 0 on a clean delete", metrics.OrphanedDeletes)
	}
}

// TestDeleteTrackService_LogsActorAndObject pins #1052: a successful track
// delete records who (user_id) deleted what (track_id) and when.
func TestDeleteTrackService_LogsActorAndObject(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	track := seedTrack(t, repo, userId, "Track", "Artist", "Album")

	if err := NewDeleteTrackService(repo, catalogtest.NewAudioStore()).Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAttrs(t, logs.find(t, "track deleted from library"), map[string]string{
		"user_id":  userId.String(),
		"track_id": track.ID.String(),
	})
}

// TestDeleteTrackService_OrphanLogCarriesActor pins #1052: the orphan-failure
// line names the user whose delete orphaned the audio, and the partial delete
// still leaves the deletion trail.
func TestDeleteTrackService_OrphanLogCarriesActor(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := catalogtest.NewAudioStore()
	store.ErrOnDelete = errors.New("s3 down")

	err := NewDeleteTrackService(repo, store).Execute(context.Background(), userId, track.ID)
	if !errors.Is(err, ErrAudioOrphaned) {
		t.Fatalf("error = %v, want ErrAudioOrphaned", err)
	}

	assertAttrs(t, logs.find(t, "orphaned audio file after track delete"), map[string]string{
		"user_id":  userId.String(),
		"track_id": track.ID.String(),
	})
	logs.find(t, "track deleted from library")
}
