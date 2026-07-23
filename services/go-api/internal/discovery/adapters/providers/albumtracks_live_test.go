package providers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// TestAlbumTracksLive_E2E exercises the real Spotify + Apple GetAlbumTracks
// against Che's actual releases. Gated by SPOTIFY_LIVE=1.
func TestAlbumTracksLive_E2E(t *testing.T) {
	if os.Getenv("SPOTIFY_LIVE") != "1" {
		t.Skip("set SPOTIFY_LIVE=1")
	}
	ctx := context.Background()
	client := &http.Client{Timeout: 15 * time.Second}

	sp := NewSpotifyAdapter(client)
	spTracks, err := sp.GetAlbumTracks(ctx, domain.ProviderSpotify, "6HBXwJApSdQ7qjr4Komyst") // Che — Empty Clip
	if err != nil {
		t.Fatalf("spotify GetAlbumTracks: %v", err)
	}
	if len(spTracks) == 0 {
		t.Fatal("spotify returned 0 tracks")
	}
	t.Logf("spotify Empty Clip: %d tracks; first %q by %q", len(spTracks), spTracks[0].Title, spTracks[0].Subtitle)

	ap := NewAppleMusicAdapter(client)
	apTracks, err := ap.GetAlbumTracks(ctx, domain.ProviderAppleMusic, "1895185526") // Che — Para'dies
	if err != nil {
		t.Fatalf("apple GetAlbumTracks: %v", err)
	}
	if len(apTracks) == 0 {
		t.Fatal("apple returned 0 tracks")
	}
	t.Logf("apple Para'dies: %d tracks; first %q by %q", len(apTracks), apTracks[0].Title, apTracks[0].Subtitle)
}
