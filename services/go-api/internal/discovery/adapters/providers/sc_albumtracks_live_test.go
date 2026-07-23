package providers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// TestSoundCloudAlbumTracksLive confirms a SoundCloud single's tracklist is the
// single itself (not a wrong Deezer album), and an EP playlist returns its tracks.
// Gated by SC_LIVE=1.
func TestSoundCloudAlbumTracksLive(t *testing.T) {
	if os.Getenv("SC_LIVE") != "1" {
		t.Skip("set SC_LIVE=1")
	}
	a := NewSoundCloudAPIAdapter(&http.Client{Timeout: 20 * time.Second}, nil)
	ctx := context.Background()

	// Find "14 HAHAHA LOL" in Che's discography to get its real SC track id.
	albums, err := a.GetArtistAlbums(ctx, domain.ProviderSoundCloud, "che")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	var singleID, epID string
	for _, r := range albums {
		if r.Title == "14 HAHAHA LOL" && len(r.Sources) > 0 {
			singleID = r.Sources[0].ExternalID
		}
		if r.Extras["record_type"] == "ep" && epID == "" && len(r.Sources) > 0 {
			epID = r.Sources[0].ExternalID
		}
	}
	if singleID == "" {
		t.Fatal("did not find '14 HAHAHA LOL' to test")
	}

	single, err := a.GetAlbumTracks(ctx, domain.ProviderSoundCloud, singleID)
	if err != nil {
		t.Fatalf("GetAlbumTracks(single): %v", err)
	}
	t.Logf("single %s -> %d track(s); first %q", singleID, len(single), titleOf(single))
	if len(single) != 1 || single[0].Title != "14 HAHAHA LOL" {
		t.Errorf("single tracklist = %+v, want just [14 HAHAHA LOL]", single)
	}

	if epID != "" {
		ep, err := a.GetAlbumTracks(ctx, domain.ProviderSoundCloud, epID)
		if err != nil {
			t.Fatalf("GetAlbumTracks(ep): %v", err)
		}
		t.Logf("ep %s -> %d track(s); first %q", epID, len(ep), titleOf(ep))
		if len(ep) == 0 {
			t.Errorf("EP tracklist empty")
		}
	}
}

func titleOf(rs []domain.SearchResult) string {
	if len(rs) == 0 {
		return ""
	}
	return rs[0].Title
}
