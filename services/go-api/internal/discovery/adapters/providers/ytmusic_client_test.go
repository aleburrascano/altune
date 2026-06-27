package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadYTMFixture(t *testing.T, name string) any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var page any
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return page
}

// TestParseYTMSearch_Unfiltered guards the regression that motivated owning the
// client: the new musicCardShelfRenderer + itemSectionRenderer layout must parse
// to non-empty, correctly-categorised results (the library returned zero).
func TestParseYTMSearch_Unfiltered(t *testing.T) {
	res := parseYTMSearch(loadYTMFixture(t, "ytmusic_search_sombr.json"))

	if len(res.Tracks) == 0 {
		t.Error("expected tracks, got none")
	}
	if len(res.Albums) == 0 {
		t.Error("expected albums, got none")
	}
	if len(res.Videos) == 0 {
		t.Error("expected videos (OMV/UGC), got none")
	}

	// The "sombr" top-result card header is the artist; it must be captured with
	// artwork, since that is what the artwork resolver and ranker need.
	var sombr *ytmArtistItem
	for _, a := range res.Artists {
		if a.Artist == "sombr" {
			sombr = a
			break
		}
	}
	if sombr == nil {
		t.Fatalf("expected artist %q in results, got %d artists", "sombr", len(res.Artists))
	}
	if sombr.BrowseID == "" {
		t.Error("artist missing browseId")
	}
	if len(sombr.Thumbnails) == 0 {
		t.Error("artist missing thumbnails")
	}

	// A track must carry a title and a videoId (the playable id).
	top := res.Tracks[0]
	if top.Title == "" || top.VideoID == "" {
		t.Errorf("track missing title/videoId: %+v", top)
	}
}

// TestParseYTMSearch_ArtistFilter covers the filtered path (still the legacy
// musicShelfRenderer shape) that the artist-artwork resolver depends on.
func TestParseYTMSearch_ArtistFilter(t *testing.T) {
	res := parseYTMSearch(loadYTMFixture(t, "ytmusic_artist_filter_sombr.json"))

	if len(res.Artists) == 0 {
		t.Fatal("expected artists from filtered search, got none")
	}
	url := pickArtistArtwork(res.Artists, "sombr", ytArtworkHeroSize)
	if url == "" {
		t.Error("expected artist artwork URL from filtered search")
	}
}

func TestYTMDurationToSeconds(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  int
	}{
		{"minutes seconds", "4:20", 260},
		{"hours", "1:02:03", 3723},
		{"empty", "", 0},
		{"view count not duration", "1.2M views", 0},
		{"zero", "0:00", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ytmDurationToSeconds(tt.in); got != tt.want {
				t.Errorf("ytmDurationToSeconds(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
