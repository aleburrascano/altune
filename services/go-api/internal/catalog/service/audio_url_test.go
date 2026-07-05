package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/catalog/domain"
)

// stubSigner turns the plain mockAudioStore into an AudioURLSigner so
// NewAudioURLService detects a signer. err makes PresignGet fail.
type stubSigner struct {
	*mockAudioStore
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
		repo := newMockTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		pending := seedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := NewAudioURLService(repo, stubSigner{mockAudioStore: newMockAudioStore()})

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
	})

	t.Run("no signer returns nothing (client proxies)", func(t *testing.T) {
		repo := newMockTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		svc := NewAudioURLService(repo, newMockAudioStore()) // plain store, not a signer

		out, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected no urls without a signer, got %d", len(out))
		}
	})

	t.Run("presign failure skips only that track", func(t *testing.T) {
		repo := newMockTrackRepo()
		ready := seedReadyTrack(t, repo, userId, "Song", "Artist", "Album", "audio/ok.opus")
		svc := NewAudioURLService(repo, stubSigner{mockAudioStore: newMockAudioStore(), err: errors.New("boom")})

		out, err := svc.Resolve(ctx, userId, []domain.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected the failed track skipped, got %d", len(out))
		}
	})
}
