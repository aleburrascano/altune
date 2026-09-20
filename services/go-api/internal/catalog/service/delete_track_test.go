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
