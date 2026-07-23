package providers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// TestSoundCloudDiscographyLive exercises GetArtistAlbums with a BRIDGE HANDLE
// ("che", as it arrives from a MusicBrainz soundcloud url-relation): it must
// resolve to the numeric id, return the typed playlists AND standalone uploads as
// singles (incl. the newest SC-exclusive drop). Gated by SC_LIVE=1.
func TestSoundCloudDiscographyLive(t *testing.T) {
	if os.Getenv("SC_LIVE") != "1" {
		t.Skip("set SC_LIVE=1")
	}
	a := NewSoundCloudAPIAdapter(&http.Client{Timeout: 20 * time.Second}, nil)
	ctx := context.Background()

	albums, err := a.GetArtistAlbums(ctx, domain.ProviderSoundCloud, "che") // handle, not numeric
	if err != nil {
		t.Fatalf("GetArtistAlbums(handle): %v", err)
	}
	var eps, singles int
	var has14 bool
	for _, r := range albums {
		switch r.Extras["record_type"] {
		case "single":
			singles++
		default:
			eps++
		}
		if r.Title == "14 HAHAHA LOL" {
			has14 = true
		}
	}
	t.Logf("discography: %d total (%d playlists/EPs, %d standalone singles); '14 HAHAHA LOL' present=%v",
		len(albums), eps, singles, has14)
	if !has14 {
		t.Errorf("expected the standalone single '14 HAHAHA LOL' in the discography")
	}
	if singles == 0 {
		t.Errorf("expected standalone singles, got none")
	}
}
