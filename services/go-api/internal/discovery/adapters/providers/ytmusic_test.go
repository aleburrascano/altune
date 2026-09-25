package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type recordingRoundTripper struct{ called bool }

func (r *recordingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.called = true
	return nil, errors.New("network call should not happen")
}

type cannedRoundTripper struct {
	status int
	body   string
	calls  int
}

func (rt *cannedRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	rt.calls++
	return &http.Response{
		StatusCode: rt.status,
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Header:     make(http.Header),
	}, nil
}

func TestYTMSearchRetry_RespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rt := &recordingRoundTripper{}
	client := &http.Client{Transport: rt}
	_, err := ytmSearchRetry(ctx, client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("want a context error, got nil")
	}
	if rt.called {
		t.Error("must not start a network call when the context is already cancelled")
	}
}

func TestYTMSearch_ThrottledResponseSurfacesStatusErrorForTheBreaker(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusTooManyRequests, body: `{"error":{"code":429,"message":"rate limited"}}`}
	client := &http.Client{Transport: rt}

	_, err := ytmSearch(context.Background(), client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("a 429 whose body parses returned nil; the breaker would record a healthy empty answer")
	}
	var status httpStatusCoder
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want an error carrying HTTPStatus() the breaker classifies as a failure", err)
	}
	if got := status.HTTPStatus(); got != http.StatusTooManyRequests {
		t.Errorf("HTTPStatus() = %d, want 429", got)
	}
}

func TestYTMSearchRetry_DoesNotRetryPermanent4xx(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusNotFound, body: `{}`}
	client := &http.Client{Transport: rt}

	_, err := ytmSearchRetry(context.Background(), client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("want a status-bearing error on a 404")
	}
	if rt.calls != 1 {
		t.Errorf("calls = %d, want 1: a permanent 404 must not be reattempted", rt.calls)
	}
}

func TestYTMSearchRetry_RetriesTransient5xx(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusServiceUnavailable, body: `{}`}
	client := &http.Client{Transport: rt}

	_, err := ytmSearchRetry(context.Background(), client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("want a status-bearing error on a 503")
	}
	if rt.calls != 2 {
		t.Errorf("calls = %d, want 2: a 503 is transient and must be retried once", rt.calls)
	}
}

func TestResizeYTThumbnail(t *testing.T) {
	tests := []struct {
		name string
		url  string
		size int
		want string
	}{
		{
			name: "album thumbnail without crop flag",
			url:  "https://yt3.googleusercontent.com/abc=w544-h544-l90-rj",
			size: 1000,
			want: "https://yt3.googleusercontent.com/abc=w1000-h1000-l90-rj",
		},
		{
			name: "artist thumbnail preserves the -p- smart-crop flag",
			url:  "https://lh3.googleusercontent.com/xyz=w120-h120-p-l90-rj",
			size: 1000,
			want: "https://lh3.googleusercontent.com/xyz=w1000-h1000-p-l90-rj",
		},
		{
			name: "unrecognized size segment returns the url unchanged",
			url:  "https://img/no-size-here",
			size: 1000,
			want: "https://img/no-size-here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resizeYTThumbnail(tt.url, tt.size); got != tt.want {
				t.Errorf("resizeYTThumbnail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPickArtistArtwork(t *testing.T) {
	thumb := func(url string) []ytmThumbnail {
		return []ytmThumbnail{{URL: "https://img/small=w60-h60-rj"}, {URL: url}}
	}

	t.Run("prefers an exact case-insensitive name match over the top result", func(t *testing.T) {
		artists := []*ytmArtistItem{
			{Artist: "Kendrick Lamar Type Beat", Thumbnails: thumb("https://img/wrong=w120-h120-rj")},
			{Artist: "kendrick lamar", Thumbnails: thumb("https://img/right=w120-h120-p-rj")},
		}
		got := pickArtistArtwork(artists, "Kendrick Lamar", 1000)
		want := "https://img/right=w1000-h1000-p-rj"
		if got != want {
			t.Errorf("got %q, want the exact-match artist resized to 1000: %q", got, want)
		}
	})

	t.Run("falls back to the top result when no exact match", func(t *testing.T) {
		artists := []*ytmArtistItem{
			{Artist: "Some Other Artist", Thumbnails: thumb("https://img/top=w120-h120-rj")},
		}
		got := pickArtistArtwork(artists, "Nonexistent", 1000)
		want := "https://img/top=w1000-h1000-rj"
		if got != want {
			t.Errorf("got %q, want the top result resized: %q", got, want)
		}
	})

	t.Run("returns empty when no artist carries a thumbnail", func(t *testing.T) {
		artists := []*ytmArtistItem{{Artist: "No Image", Thumbnails: nil}}
		if got := pickArtistArtwork(artists, "No Image", 1000); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns empty for no artists", func(t *testing.T) {
		if got := pickArtistArtwork(nil, "Anyone", 1000); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestYouTubeMusicArtworkResolver_NonArtistKindIsNoop(t *testing.T) {
	r := NewYouTubeMusicArtworkResolver(nil)
	for _, kind := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindAlbum} {
		got, err := r.Resolve(context.Background(), kind, "Some Title", "Some Subtitle", "")
		if err != nil {
			t.Errorf("kind %v: unexpected error %v", kind, err)
		}
		if got != "" {
			t.Errorf("kind %v: got %q, want empty (artist-only resolver)", kind, got)
		}
	}
}

func TestMapYTMusicVideo(t *testing.T) {
	v := &ytmVideo{
		VideoID:  "abc123",
		Title:    "Obscure Track",
		Artists:  []ytmArtistRef{{Name: "Underground Artist"}},
		Duration: 200,
		Thumbnails: []ytmThumbnail{
			{URL: "https://img/small"},
			{URL: "https://img/large"},
		},
	}

	got := mapYTMusicVideo(v)

	if got.Kind != domain.ResultKindTrack {
		t.Errorf("Kind = %v, want track", got.Kind)
	}
	if got.Title != "Obscure Track" {
		t.Errorf("Title = %q, want %q", got.Title, "Obscure Track")
	}
	if got.Subtitle != "Underground Artist" {
		t.Errorf("Subtitle = %q, want %q", got.Subtitle, "Underground Artist")
	}
	if got.ImageURL != "https://img/large" {
		t.Errorf("ImageURL = %q, want the largest thumbnail", got.ImageURL)
	}
	if len(got.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1", len(got.Sources))
	}
	src := got.Sources[0]
	if src.Provider != domain.ProviderYouTube || src.ExternalID != "abc123" {
		t.Errorf("source = %+v, want youtube/abc123", src)
	}
	if src.URL != "https://music.youtube.com/watch?v=abc123" {
		t.Errorf("source URL = %q", src.URL)
	}
	if got.Extras["duration"] != 200 {
		t.Errorf("duration extra = %v, want 200", got.Extras["duration"])
	}
}

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
	if tracks == 0 || albums == 0 || artists == 0 {
		t.Errorf("kinds mapped: tracks=%d albums=%d artists=%d, want all non-zero", tracks, albums, artists)
	}
}

func TestYouTubeMusicAdapter_Search_retriesOn403HTML(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
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

func TestYTMSearch_jsonBodyOn500SurfacesStatusErrorForTheBreaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	adapter := NewYouTubeMusicAdapter(&redirectTransport{targetURL: srv.URL})
	_, err := adapter.Search(context.Background(), "q", trackKinds())
	if err == nil {
		t.Fatal("a JSON-bodied 500 returned success; the breaker would record a healthy empty answer for a down provider")
	}
	var status httpStatusCoder
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want an error carrying HTTPStatus() the breaker classifies as a failure", err)
	}
	if got := status.HTTPStatus(); got != http.StatusInternalServerError {
		t.Errorf("HTTPStatus() = %d, want 500", got)
	}
}

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
	albums, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderYouTube, "SOMBR")
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
	if al.RecordType != "Album" {
		t.Errorf("record_type = %v, want the first byline run", al.RecordType)
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

func TestYouTubeMusicArtworkResolver_Resolve(t *testing.T) {
	t.Run("artist image resized to hero", func(t *testing.T) {
		srv := serveYTMFixture(t, "ytmusic_artist_filter_sombr.json")
		defer srv.Close()

		r := NewYouTubeMusicArtworkResolver(&redirectTransport{targetURL: srv.URL})
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "sombr", "", "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if url == "" {
			t.Fatal("expected an artist artwork URL from the fixture")
		}
		if !strings.Contains(url, "w1000-h1000") {
			t.Errorf("url = %q, want the w1000-h1000 hero resize", url)
		}
	})

	t.Run("search error degrades to empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<html>denied</html>`))
		}))
		defer srv.Close()

		r := NewYouTubeMusicArtworkResolver(&redirectTransport{targetURL: srv.URL})
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "sombr", "", "")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil) — the chain degrades", url, err)
		}
	})

	t.Run("empty title is a no-op", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected for an empty title")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		r := NewYouTubeMusicArtworkResolver(&redirectTransport{targetURL: srv.URL})
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "", "", "")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
		}
	})
}
