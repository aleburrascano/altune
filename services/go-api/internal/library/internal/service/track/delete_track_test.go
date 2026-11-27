package track_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"strings"
	"testing"
)

const sharedAudioRef = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/artist/abbey road/come together.mp3"

func TestNewDeleteTrackService_PanicsWithoutACollaborator(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	store := library.NewInMemoryAudioStore()
	pub := events.NoopPublisher()
	metrics := &library.InMemoryAudioStoreMetrics{}
	queue := library.NewInMemoryOrphanedAudioQueue(repo, clock.Now)
	tests := []struct {
		name string
		make func()
	}{
		{"repository", func() { library.NewDeleteTrackService(nil, store, pub, metrics, queue) }},
		{"events", func() { library.NewDeleteTrackService(repo, store, nil, metrics, queue) }},
		{"metrics", func() { library.NewDeleteTrackService(repo, store, pub, nil, queue) }},
		{"orphan recorder", func() { library.NewDeleteTrackService(repo, store, pub, metrics, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("construction without %s did not panic", tt.name)
				}
			}()
			tt.make()
		})
	}
}

func TestDeleteTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*library.InMemoryTrackRepo) library.TrackId
		wantErr error
	}{
		{
			name: "existing track is deleted",
			setup: func(repo *library.InMemoryTrackRepo) library.TrackId {
				track := librarytest.SeedTrack(t, repo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr: nil,
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *library.InMemoryTrackRepo) library.TrackId {
				return librarytest.NewTrackId()
			},
			wantErr: library.ErrTrackNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *library.InMemoryTrackRepo) library.TrackId {
				track := librarytest.SeedTrack(t, repo, userId, "Track", "Artist", "Album")
				repo.ErrOnDelete = errRepo
				return track.ID
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			trackId := tt.setup(repo)
			svc := library.NewDeleteTrackService(repo, library.NewInMemoryAudioStore(), events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now))

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

func TestDeleteTrack_KeepsAudioSharedWithAnotherTrack(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	store := library.NewInMemoryAudioStore()
	userId := librarytest.UserId()
	deleted := librarytest.SeedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road", sharedAudioRef)
	kept := librarytest.SeedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road (Remastered)", sharedAudioRef)
	assertCollidesOnOneStorageKey(t, deleted, kept)
	store.Seed(sharedAudioRef, []byte("audio"))

	if err := library.NewDeleteTrackService(repo, store, events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)).Execute(context.Background(), userId, deleted.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, ok := store.Files[sharedAudioRef]; !ok {
		t.Fatalf("audio %q deleted while track %s still references it", sharedAudioRef, kept.ID)
	}
}

func TestDeleteTrack_DeletesAudioOnTheLastReference(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	store := library.NewInMemoryAudioStore()
	userId := librarytest.UserId()
	only := librarytest.SeedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road", sharedAudioRef)
	store.Seed(sharedAudioRef, []byte("audio"))

	if err := library.NewDeleteTrackService(repo, store, events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)).Execute(context.Background(), userId, only.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, ok := store.Files[sharedAudioRef]; ok {
		t.Fatalf("audio %q kept after its last reference was deleted", sharedAudioRef)
	}
}

func TestDeleteTrack_QueuesTheOrphanWhenTheUsageCheckFails(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	store := library.NewInMemoryAudioStore()
	queue := library.NewInMemoryOrphanedAudioQueue(repo, clock.Now)
	userId := librarytest.UserId()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Come Together", "Artist", "Abbey Road", sharedAudioRef)
	store.Seed(sharedAudioRef, []byte("audio"))
	repo.ErrOnGetBy = errors.New("db error")

	err := library.NewDeleteTrackService(repo, store, events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, queue).
		Execute(context.Background(), userId, track.ID)

	if !errors.Is(err, library.ErrAudioOrphaned) {
		t.Fatalf("delete err = %v, want ErrAudioOrphaned", err)
	}
	if _, ok := store.Files[sharedAudioRef]; !ok {
		t.Fatalf("audio %q deleted while its usage was unknown", sharedAudioRef)
	}
	if _, ok := queue.Orphans[sharedAudioRef]; !ok {
		t.Fatalf("audio %q left behind without being queued for the sweep", sharedAudioRef)
	}
}

func assertCollidesOnOneStorageKey(t *testing.T, a, b *library.Track) {
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
	userId := librarytest.UserId()

	repo := library.NewInMemoryTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := library.NewInMemoryAudioStore()
	store.ErrOnDelete = errors.New("s3 down")
	metrics := &library.InMemoryAudioStoreMetrics{}
	svc := library.NewDeleteTrackService(repo, store, events.NoopPublisher(), metrics, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now))

	err := svc.Execute(ctx, userId, track.ID)
	if err == nil {
		t.Fatal("expected ErrAudioOrphaned, got nil (orphan silently swallowed as success)")
	}
	if !errors.Is(err, library.ErrAudioOrphaned) {
		t.Fatalf("error = %v, want ErrAudioOrphaned", err)
	}
	if metrics.OrphanedDeletes != 1 {
		t.Errorf("orphaned-delete metric = %d, want 1 so operators can alert on orphaning", metrics.OrphanedDeletes)
	}
}

func TestDeleteTrackService_NoOrphanMetricOnCleanDelete(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	repo := library.NewInMemoryTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
	store := library.NewInMemoryAudioStore()
	store.Seed("audio/ok.opus", []byte("data"))
	metrics := &library.InMemoryAudioStoreMetrics{}
	svc := library.NewDeleteTrackService(repo, store, events.NoopPublisher(), metrics, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now))

	if err := svc.Execute(ctx, userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.OrphanedDeletes != 0 {
		t.Errorf("orphaned-delete metric = %d, want 0 on a clean delete", metrics.OrphanedDeletes)
	}
}

func TestDeleteTrackService_LogsTheTrack(t *testing.T) {
	logs := library.CaptureAuditLogs(t)
	userId := librarytest.UserId()
	repo := library.NewInMemoryTrackRepo()
	track := librarytest.SeedTrack(t, repo, userId, "Track", "Artist", "Album")

	if err := library.NewDeleteTrackService(repo, library.NewInMemoryAudioStore(), events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)).Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	library.AssertAttrs(t, logs.Find(t, "track deleted from library"), map[string]string{
		"track_id": track.ID.String(),
	})
}

func TestDeleteTrackService_OrphanLogNamesTheTrackAndAudio(t *testing.T) {
	logs := library.CaptureAuditLogs(t)
	userId := librarytest.UserId()
	repo := library.NewInMemoryTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := library.NewInMemoryAudioStore()
	store.ErrOnDelete = errors.New("s3 down")

	err := library.NewDeleteTrackService(repo, store, events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)).Execute(context.Background(), userId, track.ID)
	if !errors.Is(err, library.ErrAudioOrphaned) {
		t.Fatalf("error = %v, want ErrAudioOrphaned", err)
	}

	library.AssertAttrs(t, logs.Find(t, "orphaned audio file after track delete"), map[string]string{
		"track_id": track.ID.String(),
	})
	logs.Find(t, "track deleted from library")
}
