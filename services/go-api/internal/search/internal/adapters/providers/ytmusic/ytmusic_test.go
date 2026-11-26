package ytmusic_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
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

type statusCoder interface{ UpstreamStatus() int }

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

func TestYouTubeMusicAdapter_Search_RespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rt := &recordingRoundTripper{}
	_, err := search.NewYouTubeMusicAdapter(rt).Search(ctx, "anything", providertest.AllKinds())

	if err == nil {
		t.Fatal("want a context error, got nil")
	}
	if rt.called {
		t.Error("must not start a network call when the context is already cancelled")
	}
}

func TestYouTubeMusicAdapter_Search_ThrottledResponseSurfacesStatusErrorForTheBreaker(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusTooManyRequests, body: `{"error":{"code":429,"message":"rate limited"}}`}

	_, err := search.NewYouTubeMusicAdapter(rt).Search(context.Background(), "anything", providertest.AllKinds())

	if err == nil {
		t.Fatal("a 429 whose body parses returned nil; the breaker would record a healthy empty answer")
	}
	var status statusCoder
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want an error carrying UpstreamStatus() the breaker classifies as a failure", err)
	}
	if got := status.UpstreamStatus(); got != http.StatusTooManyRequests {
		t.Errorf("UpstreamStatus() = %d, want 429", got)
	}
}

func TestYouTubeMusicAdapter_Search_DoesNotRetryPermanent4xx(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusNotFound, body: `{}`}

	_, err := search.NewYouTubeMusicAdapter(rt).Search(context.Background(), "anything", providertest.AllKinds())

	if err == nil {
		t.Fatal("want a status-bearing error on a 404")
	}
	if rt.calls != 1 {
		t.Errorf("calls = %d, want 1: a permanent 404 must not be reattempted", rt.calls)
	}
}

func TestYouTubeMusicAdapter_Search_LeavesTransientRetriesToTheEgressTransport(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		rt := &cannedRoundTripper{status: status, body: `{}`}

		_, err := search.NewYouTubeMusicAdapter(rt).Search(context.Background(), "anything", providertest.AllKinds())

		if err == nil {
			t.Fatalf("want a status-bearing error on a %d", status)
		}
		if rt.calls != 1 {
			t.Errorf("status %d: calls = %d, want 1: the egress transport already retries it, so the adapter must not stack a second retry loop on top", status, rt.calls)
		}
	}
}

func searchTracks(t *testing.T, items ...any) []search.SearchResult {
	t.Helper()
	srv := servePage(t, ytmShelfPage(t, items...))
	results, err := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL}).
		Search(context.Background(), "q", providertest.TrackKinds())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return results
}

func resolveArtistArtwork(t *testing.T, name string, artists ...any) string {
	t.Helper()
	srv := servePage(t, ytmShelfPage(t, artists...))
	url, err := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL}).
		Resolve(context.Background(), search.ResultKindArtist, name, "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return url
}

func TestYouTubeMusicArtworkResolver_Resolve_ResizesTheThumbnailToTheHeroSize(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "album thumbnail without crop flag",
			url:  "https://yt3.googleusercontent.com/abc=w544-h544-l90-rj",
			want: "https://yt3.googleusercontent.com/abc=w1000-h1000-l90-rj",
		},
		{
			name: "artist thumbnail preserves the -p- smart-crop flag",
			url:  "https://lh3.googleusercontent.com/xyz=w120-h120-p-l90-rj",
			want: "https://lh3.googleusercontent.com/xyz=w1000-h1000-p-l90-rj",
		},
		{
			name: "unrecognized size segment returns the url unchanged",
			url:  "https://img/no-size-here",
			want: "https://img/no-size-here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveArtistArtwork(t, "Nas", ytmArtist("Nas", "UCnas", tt.url)); got != tt.want {
				t.Errorf("Resolve = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestYouTubeMusicArtworkResolver_Resolve_PicksTheArtist(t *testing.T) {
	const small = "https://img/small=w60-h60-rj"

	t.Run("prefers an exact case-insensitive name match over the top result", func(t *testing.T) {
		got := resolveArtistArtwork(t, "Kendrick Lamar",
			ytmArtist("Kendrick Lamar Type Beat", "UCwrong", small, "https://img/wrong=w120-h120-rj"),
			ytmArtist("kendrick lamar", "UCright", small, "https://img/right=w120-h120-p-rj"))
		want := "https://img/right=w1000-h1000-p-rj"
		if got != want {
			t.Errorf("got %q, want the exact-match artist resized to 1000: %q", got, want)
		}
	})

	t.Run("falls back to the top result when no exact match", func(t *testing.T) {
		got := resolveArtistArtwork(t, "Nonexistent",
			ytmArtist("Some Other Artist", "UCtop", small, "https://img/top=w120-h120-rj"))
		want := "https://img/top=w1000-h1000-rj"
		if got != want {
			t.Errorf("got %q, want the top result resized: %q", got, want)
		}
	})

	t.Run("returns empty when no artist carries a thumbnail", func(t *testing.T) {
		if got := resolveArtistArtwork(t, "No Image", ytmArtist("No Image", "UCnone")); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns empty for no artists", func(t *testing.T) {
		if got := resolveArtistArtwork(t, "Anyone"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestYouTubeMusicArtworkResolver_NonArtistKindIsNoop(t *testing.T) {
	r := search.NewYouTubeMusicArtworkLookup(nil)
	for _, kind := range []search.ResultKind{search.ResultKindTrack, search.ResultKindAlbum} {
		got, err := r.Resolve(context.Background(), kind, "Some Title", "Some Subtitle", "")
		if err != nil {
			t.Errorf("kind %v: unexpected error %v", kind, err)
		}
		if got != "" {
			t.Errorf("kind %v: got %q, want empty (artist-only resolver)", kind, got)
		}
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

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	results, err := adapter.Search(context.Background(), "sombr", providertest.AllKinds())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var tracks, albums, artists int
	sawSombr := false
	for _, r := range results {
		switch r.Kind {
		case search.ResultKindTrack:
			tracks++
			if len(r.Sources) != 1 || r.Sources[0].Provider != search.ProviderYouTube {
				t.Errorf("track source = %+v", r.Sources)
			}
			if r.Sources[0].ExternalID == "" {
				t.Error("track missing videoId external id")
			}
		case search.ResultKindAlbum:
			albums++
			if r.Sources[0].URL == "" || r.Sources[0].ExternalID == "" {
				t.Errorf("album missing browse ref: %+v", r.Sources[0])
			}
		case search.ResultKindArtist:
			artists++
			if r.Title == "sombr" {
				sawSombr = true
				if r.Sources[0].ExternalID == "" || r.ImageURL == "" {
					t.Errorf("artist sombr missing browseId or thumbnail: %+v", r)
				}
			}
		case search.ResultKindUnknown, search.ResultKindPlaylist:
			t.Errorf("unexpected result kind %v: %+v", r.Kind, r)
		}
	}
	if !sawSombr {
		t.Errorf("expected artist %q in results", "sombr")
	}
	if top := results[0]; top.Kind == search.ResultKindTrack && (top.Title == "" || top.Sources[0].ExternalID == "") {
		t.Errorf("top track missing title or videoId: %+v", top)
	}
	if tracks == 0 || albums == 0 || artists == 0 {
		t.Errorf("kinds mapped: tracks=%d albums=%d artists=%d, want all non-zero", tracks, albums, artists)
	}
}

func TestYouTubeMusicAdapter_Search_wrongShapeBodyIsEmptyNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["not","an","object"]`))
	}))
	defer srv.Close()

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	results, err := adapter.Search(context.Background(), "q", providertest.AllKinds())
	if err != nil {
		t.Fatalf("Search = (%v, %v), want (nil, nil): a well-formed body of the wrong shape must not fail provider search", results, err)
	}
	if results != nil {
		t.Errorf("results = %+v, want nil", results)
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

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	if _, err := adapter.Search(context.Background(), "q", providertest.TrackKinds()); err != nil {
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

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	_, err := adapter.Search(context.Background(), "q", providertest.TrackKinds())
	if err == nil {
		t.Fatal("expected an error when both attempts 403")
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want exactly 2 attempts (one retry, no more)", hits.Load())
	}
}

func TestYouTubeMusicAdapter_Search_jsonBodyOn500SurfacesStatusErrorForTheBreaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	_, err := adapter.Search(context.Background(), "q", providertest.TrackKinds())
	if err == nil {
		t.Fatal("a JSON-bodied 500 returned success; the breaker would record a healthy empty answer for a down provider")
	}
	var status statusCoder
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want an error carrying UpstreamStatus() the breaker classifies as a failure", err)
	}
	if got := status.UpstreamStatus(); got != http.StatusInternalServerError {
		t.Errorf("UpstreamStatus() = %d, want 500", got)
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

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	albums, err := adapter.GetArtistAlbums(context.Background(), search.ProviderYouTube, "SOMBR")
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

func TestYouTubeMusicAdapter_GetArtistAlbums_wrongShapeBodyIsEmptyNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["not","an","object"]`))
	}))
	defer srv.Close()

	adapter := search.NewYouTubeMusicAdapter(&providertest.RedirectTransport{TargetURL: srv.URL})
	albums, err := adapter.GetArtistAlbums(context.Background(), search.ProviderYouTube, "sombr")
	if err != nil {
		t.Fatalf("GetArtistAlbums = (%v, %v), want (nil, nil)", albums, err)
	}
	if albums != nil {
		t.Errorf("albums = %+v, want nil", albums)
	}
}

func TestYouTubeMusicAdapter_meta(t *testing.T) {
	adapter := search.NewYouTubeMusicAdapter(nil)
	if adapter.Name() != search.ProviderYouTube {
		t.Errorf("Name = %v", adapter.Name())
	}
	kinds := adapter.SupportedKinds()
	if !kinds[search.ResultKindTrack] || !kinds[search.ResultKindAlbum] || !kinds[search.ResultKindArtist] {
		t.Errorf("SupportedKinds = %v, want all three", kinds)
	}
	if adapter.SearchTimeout() <= 0 {
		t.Error("SearchTimeout must be positive")
	}
	if search.NewYouTubeMusicArtworkLookup(nil).ArtworkSource() != "ytmusic" {
		t.Error("ArtworkSource mismatch")
	}
}

func TestYouTubeMusicArtworkResolver_Resolve(t *testing.T) {
	t.Run("artist image resized to hero", func(t *testing.T) {
		srv := serveYTMFixture(t, "ytmusic_artist_filter_sombr.json")
		defer srv.Close()

		r := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL})
		url, err := r.Resolve(context.Background(), search.ResultKindArtist, "sombr", "", "")
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

	t.Run("search error is unavailable not empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<html>denied</html>`))
		}))
		defer srv.Close()

		r := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL})
		url, err := r.Resolve(context.Background(), search.ResultKindArtist, "sombr", "", "")
		if !errors.Is(err, search.ErrArtworkUnavailable) {
			t.Fatalf("Resolve = (%q, %v), want ports.ErrArtworkUnavailable", url, err)
		}
		if url != "" {
			t.Errorf("url = %q, want empty on 403", url)
		}
	})

	t.Run("empty title is a no-op", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected for an empty title")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		r := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL})
		url, err := r.Resolve(context.Background(), search.ResultKindArtist, "", "", "")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
		}
	})
}

func TestYouTubeMusicArtworkResolver_Resolve_ProviderFailureIsUnavailable(t *testing.T) {
	for name, handler := range providertest.ArtworkFailureHandlers() {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			r := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL})
			url, err := r.Resolve(context.Background(), search.ResultKindArtist, "sombr", "", "")
			if url != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
				t.Fatalf("Resolve on %s = (%q, %v), want (\"\", ErrArtworkUnavailable)", name, url, err)
			}
			if !strings.Contains(err.Error(), "ytmusic") {
				t.Errorf("err = %q, want it to name the ytmusic source", err)
			}
		})
	}
}

func TestYouTubeMusicArtworkResolver_Resolve_VerifiedMissIsEmptyWithoutError(t *testing.T) {
	handlers := providertest.ArtworkVerifiedMissHandlers()
	handlers["200 with no results"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"contents":{}}`))
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			r := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL})
			url, err := r.Resolve(context.Background(), search.ResultKindArtist, "sombr", "", "")
			if url != "" || err != nil {
				t.Errorf("Resolve on %s = (%q, %v), want (\"\", nil)", name, url, err)
			}
		})
	}
}

func TestYouTubeMusicArtworkResolver_Resolve_CancelledMidRequestIsUnavailable(t *testing.T) {
	srv, ctx := providertest.CancelMidRequestServer(t)

	r := search.NewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL})
	url, err := r.Resolve(ctx, search.ResultKindArtist, "sombr", "", "")
	if url != "" || !errors.Is(err, search.ErrArtworkUnavailable) || !errors.Is(err, context.Canceled) {
		t.Errorf("Resolve cancelled mid-request = (%q, %v), want ErrArtworkUnavailable wrapping context.Canceled", url, err)
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  search.ProviderKey
		want string
	}{
		{search.NewYouTubeMusicArtworkLookup(nil).ArtworkSource(), "ytmusic"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}
