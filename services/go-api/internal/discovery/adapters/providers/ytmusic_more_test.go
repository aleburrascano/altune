package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// serveYTMFixture returns an httptest server replaying the named testdata file
// for every request.
func serveYTMFixture(t *testing.T, name string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
}

func TestYouTubeMusicAdapter_Search_mapsAllKinds(t *testing.T) {
	srv := serveYTMFixture(t, "ytmusic_search_sombr.json")
	defer srv.Close()

	adapter := NewYouTubeMusicAdapter(&redirectTransport{targetURL: srv.URL})
	results, err := adapter.Search(context.Background(), "sombr", allKinds())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var tracks, albums, artists int
	for _, r := range results {
		switch r.Kind {
		case domain.ResultKindTrack:
			tracks++
			if len(r.Sources) != 1 || r.Sources[0].Provider != domain.ProviderYouTube {
				t.Errorf("track source = %+v", r.Sources)
			}
			if r.Sources[0].ExternalID == "" {
				t.Error("track missing videoId external id")
			}
		case domain.ResultKindAlbum:
			albums++
			if r.Sources[0].URL == "" || r.Sources[0].ExternalID == "" {
				t.Errorf("album missing browse ref: %+v", r.Sources[0])
			}
		case domain.ResultKindArtist:
			artists++
		}
	}
	// Videos (OMV/UGC) must be folded into tracks (Pattern-C coverage fix), so
	// tracks > pure-ATV count; all three kinds must be represented.
	if tracks == 0 || albums == 0 || artists == 0 {
		t.Errorf("kinds mapped: tracks=%d albums=%d artists=%d, want all non-zero", tracks, albums, artists)
	}
}

func TestYouTubeMusicAdapter_Search_retriesOn403HTML(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			// The intermittent rate-limit: HTTP 403 with an HTML body that is not
			// valid JSON — must surface as an error and trigger the single retry.
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<html><body>Access denied</body></html>`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"contents":{}}`))
	}))
	defer srv.Close()

	adapter := NewYouTubeMusicAdapter(&redirectTransport{targetURL: srv.URL})
	if _, err := adapter.Search(context.Background(), "q", trackKinds()); err != nil {
		t.Fatalf("Search must succeed on the retry after a transient 403: %v", err)
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want 2 (one 403 + one retry)", hits.Load())
	}
}

func TestYouTubeMusicAdapter_Search_persistent403IsError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html>Access denied</html>`))
	}))
	defer srv.Close()

	adapter := NewYouTubeMusicAdapter(&redirectTransport{targetURL: srv.URL})
	_, err := adapter.Search(context.Background(), "q", trackKinds())
	if err == nil {
		t.Fatal("expected an error when both attempts 403")
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want exactly 2 attempts (one retry, no more)", hits.Load())
	}
}

// AIDEV-NOTE: KNOWN silent-zero — ytmSearch never checks the HTTP status; any
// response whose body decodes as JSON (here a 500 with `{}`) parses to zero
// results and reports success. The status only surfaces when the body is
// non-JSON (the 403 HTML case). This test PINS the current behaviour; if the
// adapter ever gains a status check, update this to expect an error.
func TestYTMSearch_jsonBodyOn500IsSilentZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	adapter := NewYouTubeMusicAdapter(&redirectTransport{targetURL: srv.URL})
	results, err := adapter.Search(context.Background(), "q", trackKinds())
	if err != nil {
		t.Fatalf("pinned behaviour: JSON-bodied 500 is a silent empty success, got error %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

// ytmAlbumShelfJSON is a minimal filtered album-search response (legacy
// musicShelfRenderer shape) with two albums by different artists.
const ytmAlbumShelfJSON = `{
  "contents": {"tabbedSearchResultsRenderer": {"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [
    {"musicShelfRenderer": {"contents": [
      {"musicResponsiveListItemRenderer": {
        "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_match", "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ALBUM"}}}},
        "flexColumns": [
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "I Barely Know Her"}]}}},
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
            {"text": "Album"},
            {"text": " • "},
            {"text": "sombr", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCsombr", "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ARTIST"}}}}},
            {"text": " • "},
            {"text": "2025"}
          ]}}}
        ],
        "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://img/w60-h60-rj", "width": 60, "height": 60}]}}}
      }},
      {"musicResponsiveListItemRenderer": {
        "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_other", "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ALBUM"}}}},
        "flexColumns": [
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Unrelated Album"}]}}},
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
            {"text": "Album"},
            {"text": " • "},
            {"text": "Someone Else", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCother", "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ARTIST"}}}}},
            {"text": " • "},
            {"text": "2019"}
          ]}}}
        ],
        "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://img/other-w60-h60-rj", "width": 60, "height": 60}]}}}
      }}
    ]}}
  ]}}}}]}}
}`

func TestYouTubeMusicAdapter_GetArtistAlbums_filtersToExactArtistName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ytmAlbumShelfJSON))
	}))
	defer srv.Close()

	adapter := NewYouTubeMusicAdapter(&redirectTransport{targetURL: srv.URL})
	albums, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderYouTube, "SOMBR") // case-insensitive
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("albums = %d, want only the exact-artist match (contamination guard)", len(albums))
	}
	al := albums[0]
	if al.Title != "I Barely Know Her" || al.Subtitle != "sombr" {
		t.Errorf("album = %+v", al)
	}
	if al.Year != 2025 {
		t.Errorf("Year = %d, want 2025 parsed from the trailing byline run", al.Year)
	}
	if al.Extras["record_type"] != "Album" {
		t.Errorf("record_type = %v, want the first byline run", al.Extras["record_type"])
	}
	if al.Sources[0].ExternalID != "MPREb_match" {
		t.Errorf("ExternalID = %q, want the browseId", al.Sources[0].ExternalID)
	}
}

func TestFallbackByline(t *testing.T) {
	runs := []any{
		map[string]any{"text": "Song"},
		map[string]any{"text": " • "},
		map[string]any{"text": "Plain Artist"},
	}
	got := fallbackByline(runs)
	if len(got) != 1 || got[0].Name != "Plain Artist" {
		t.Errorf("fallbackByline = %+v, want the plain third-run artist", got)
	}
	if fallbackByline(runs[:2]) != nil {
		t.Error("short runs must yield nil")
	}
	divider := []any{
		map[string]any{"text": "Song"},
		map[string]any{"text": " • "},
		map[string]any{"text": " • "},
	}
	if fallbackByline(divider) != nil {
		t.Error("a bare divider third run must yield nil")
	}
}

func TestParseYTMTrailingDuration_emptyRuns(t *testing.T) {
	if got := parseYTMTrailingDuration(nil); got != 0 {
		t.Errorf("empty runs = %d, want 0", got)
	}
}

func TestYouTubeMusicAdapter_meta(t *testing.T) {
	adapter := NewYouTubeMusicAdapter(nil)
	if adapter.Name() != domain.ProviderYouTube {
		t.Errorf("Name = %v", adapter.Name())
	}
	kinds := adapter.SupportedKinds()
	if !kinds[domain.ResultKindTrack] || !kinds[domain.ResultKindAlbum] || !kinds[domain.ResultKindArtist] {
		t.Errorf("SupportedKinds = %v, want all three", kinds)
	}
	if adapter.SearchTimeout() <= 0 {
		t.Error("SearchTimeout must be positive")
	}
	if (&YouTubeMusicArtworkResolver{}).ArtworkSource() != "ytmusic" {
		t.Error("ArtworkSource mismatch")
	}
}
