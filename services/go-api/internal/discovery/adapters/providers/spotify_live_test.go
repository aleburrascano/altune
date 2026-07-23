package providers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// TestSpotifyContentLive_E2E exercises the REAL adapter (no test overrides)
// against live Spotify, confirming the pathfinder content path returns data.
// Gated by SPOTIFY_LIVE=1; not part of the regression suite.
func TestSpotifyContentLive_E2E(t *testing.T) {
	if os.Getenv("SPOTIFY_LIVE") != "1" {
		t.Skip("set SPOTIFY_LIVE=1 to run the live Spotify content E2E")
	}
	a := NewSpotifyAdapter(&http.Client{Timeout: 15 * time.Second})
	ctx := context.Background()
	const weeknd = "1Xyo4u8uXC1ZmMpatF05PJ"

	albums, err := a.GetArtistAlbums(ctx, domain.ProviderSpotify, weeknd)
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(albums) == 0 {
		t.Fatal("GetArtistAlbums returned 0 albums")
	}
	t.Logf("albums: %d; first: %q date=%q tracks=%d type=%v img=%t url=%q",
		len(albums), albums[0].Title, albums[0].ReleaseDate, albums[0].TrackCount,
		albums[0].Extras["record_type"], albums[0].ImageURL != "", albums[0].Sources[0].URL)

	tracks, err := a.GetArtistTopTracks(ctx, domain.ProviderSpotify, weeknd)
	if err != nil {
		t.Fatalf("GetArtistTopTracks error = %v", err)
	}
	if len(tracks) == 0 {
		t.Fatal("GetArtistTopTracks returned 0 tracks")
	}
	t.Logf("topTracks: %d; first: %q by %q dur=%ds img=%t",
		len(tracks), tracks[0].Title, tracks[0].Subtitle, tracks[0].Duration, tracks[0].ImageURL != "")
}
