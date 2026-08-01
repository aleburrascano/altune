package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
)

type stubSigner struct {
	*catalogtest.AudioStore
	err error
}

func (s stubSigner) PresignGet(_ context.Context, audioRef string, _ time.Duration) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return "https://signed.example/" + audioRef, nil
}

func TestAudioURLService_Resolve(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	t.Run("ready tracks get signed urls, non-streamable are skipped", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		pending := seedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := NewAudioURLService(repo, stubSigner{AudioStore: catalogtest.NewAudioStore()})

		out, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID, pending.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("expected 1 url (pending skipped), got %d", len(out))
		}
		if out[0].TrackID != ready.ID {
			t.Errorf("url track = %v, want %v", out[0].TrackID, ready.ID)
		}
		if out[0].URL != "https://signed.example/audio/ok.opus" {
			t.Errorf("url = %q, unexpected", out[0].URL)
		}
		if !out[0].ExpiresAt.After(time.Now()) {
			t.Error("expires_at should be in the future")
		}
		if out[0].Version != ready.AudioVersion {
			t.Errorf("version = %q, want %q", out[0].Version, ready.AudioVersion)
		}
		if out[0].Version == "" {
			t.Error("a ready track must carry a non-empty audio version")
		}
	})

	t.Run("a re-acquired track resolves under a new version so a cached client copy stops matching", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		svc := NewAudioURLService(repo, stubSigner{AudioStore: catalogtest.NewAudioStore()})

		before, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := ready.MarkReady("audio/ok.opus"); err != nil {
			t.Fatalf("re-acquire: %v", err)
		}
		if err := repo.Update(ctx, ready); err != nil {
			t.Fatalf("update: %v", err)
		}

		after, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if after[0].URL != before[0].URL {
			t.Fatalf("this case is only meaningful while the object key is stable: %q vs %q", before[0].URL, after[0].URL)
		}
		if after[0].Version == before[0].Version {
			t.Errorf("version stayed %q across a re-acquisition — a client keyed on it would keep serving the old audio", after[0].Version)
		}
	})

	t.Run("a track acquired before the column existed resolves with an empty version", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		ready.AudioVersion = ""
		if err := repo.Update(ctx, ready); err != nil {
			t.Fatalf("update: %v", err)
		}
		svc := NewAudioURLService(repo, stubSigner{AudioStore: catalogtest.NewAudioStore()})

		out, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("expected the track to still resolve, got %d urls", len(out))
		}
		if out[0].Version != "" {
			t.Errorf("version = %q, want empty so the client keeps its existing copy", out[0].Version)
		}
	})

	t.Run("no signer returns nothing (client proxies)", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		svc := NewAudioURLService(repo, catalogtest.NewAudioStore())

		out, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected no urls without a signer, got %d", len(out))
		}
	})

	t.Run("presign failure skips only that track", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		svc := NewAudioURLService(repo, stubSigner{AudioStore: catalogtest.NewAudioStore(), err: errors.New("boom")})

		out, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected the failed track skipped, got %d", len(out))
		}
	})
}
