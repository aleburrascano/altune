package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// newTestSoundCloudAPI builds an adapter whose api-v2 base points at srv and
// whose client_id is pre-seeded, so Search/Resolve skip homepage resolution.
func newTestSoundCloudAPI(srv *httptest.Server, fallback searchFallback) *SoundCloudAPIAdapter {
	a := NewSoundCloudAPIAdapter(srv.Client(), fallback)
	a.baseURL = srv.URL
	a.resolver.cached = "seededclientid0000000000000000000"
	return a
}

func trackKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

func TestSoundCloudAPIAdapter_Name(t *testing.T) {
	a := NewSoundCloudAPIAdapter(http.DefaultClient, nil)
	if got := a.Name(); got != domain.ProviderSoundCloud {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderSoundCloud)
	}
}

func TestSoundCloudAPIAdapter_SupportedKinds(t *testing.T) {
	a := NewSoundCloudAPIAdapter(http.DefaultClient, nil)
	kinds := a.SupportedKinds()
	if !kinds[domain.ResultKindTrack] {
		t.Error("expected track supported")
	}
	if kinds[domain.ResultKindAlbum] || kinds[domain.ResultKindArtist] {
		t.Error("expected album/artist NOT supported")
	}
}

func TestSoundCloudAPIAdapter_SearchTimeout(t *testing.T) {
	a := NewSoundCloudAPIAdapter(http.DefaultClient, nil)
	if got := a.SearchTimeout(); got != scSearchTimeout {
		t.Errorf("SearchTimeout() = %v, want %v", got, scSearchTimeout)
	}
}

func TestSoundCloudAPIAdapter_Search_UnsupportedKinds(t *testing.T) {
	a := NewSoundCloudAPIAdapter(http.DefaultClient, nil)
	results, err := a.Search(context.Background(), "q", map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %d", len(results))
	}
}

func TestSoundCloudAPIAdapter_Search_MapsRichMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/tracks") && r.URL.Path != "/search/tracks" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("client_id") == "" {
			t.Error("expected client_id query param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"collection": [
				{
					"id": 12345,
					"kind": "track",
					"title": "Olympics",
					"permalink_url": "https://soundcloud.com/kencarson/olympics",
					"duration": 180000,
					"genre": "Rap",
					"artwork_url": "https://i1.sndcdn.com/artworks-abc-large.jpg",
					"playback_count": 999000,
					"likes_count": 4200,
					"reposts_count": 88,
					"user": { "username": "Ken Carson" }
				},
				{ "id": 0, "title": "skip me — no id" },
				{ "id": 7, "kind": "playlist", "title": "skip me — not a track" }
			],
			"next_href": ""
		}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	results, err := a.Search(context.Background(), "Ken Carson Olympics", trackKinds())
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 mapped track (others skipped), got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind = %v, want track", r.Kind)
	}
	if r.Title != "Olympics" {
		t.Errorf("title = %q", r.Title)
	}
	if r.Subtitle != "Ken Carson" {
		t.Errorf("subtitle = %q, want uploader username", r.Subtitle)
	}
	if r.ImageURL != "https://i1.sndcdn.com/artworks-abc-t500x500.jpg" {
		t.Errorf("artwork not upgraded to 500px: %q", r.ImageURL)
	}
	if len(r.Sources) != 1 || r.Sources[0].Provider != domain.ProviderSoundCloud {
		t.Fatalf("source not soundcloud: %+v", r.Sources)
	}
	if r.Sources[0].ExternalID != "12345" {
		t.Errorf("external id = %q, want 12345", r.Sources[0].ExternalID)
	}
	if got := r.Extras["duration"]; got != 180.0 {
		t.Errorf("duration = %v, want 180.0 seconds", got)
	}
	if got := r.Extras["playback_count"]; got != int64(999000) {
		t.Errorf("playback_count = %v (%T), want int64 999000", got, got)
	}
	if r.Extras["genre"] != "Rap" {
		t.Errorf("genre = %v", r.Extras["genre"])
	}
	if r.Extras["likes_count"] != int64(4200) {
		t.Errorf("likes_count = %v", r.Extras["likes_count"])
	}
}

func TestSoundCloudAPIAdapter_Search_Paginates(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "20" {
			_, _ = w.Write([]byte(`{"collection":[{"id":2,"kind":"track","title":"B","user":{"username":"u"}}],"next_href":""}`))
			return
		}
		// First page: 20 tracks + a next_href pointing at offset=20.
		var b strings.Builder
		b.WriteString(`{"collection":[`)
		for i := 0; i < 20; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"id":1,"kind":"track","title":"A","user":{"username":"u"}}`)
		}
		b.WriteString(`],"next_href":"` + srv0URL(r) + `/search/tracks?q=x&offset=20"}`)
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	results, err := a.Search(context.Background(), "x", trackKinds())
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 page fetches, got %d", calls)
	}
	if len(results) != 21 {
		t.Errorf("expected 21 results across 2 pages, got %d", len(results))
	}
}

// srv0URL reconstructs the test server's base URL from a request, so the
// next_href the handler emits loops back to the same server.
func srv0URL(r *http.Request) string {
	scheme := "http"
	return scheme + "://" + r.Host
}

func TestSoundCloudAPIAdapter_AuthFailure_ReResolvesClientID(t *testing.T) {
	var searchCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchCalls++
		// First search call: stale client_id → 401. Second: success.
		if searchCalls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collection":[{"id":9,"kind":"track","title":"OK","user":{"username":"u"}}],"next_href":""}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	// The post-401 invalidate→re-resolve path needs a homepage+bundle to scrape
	// a fresh client_id from.
	bundle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`x=1,client_id:"abcdefghijklmnopqrstuvwxyz012345",y=2`))
	}))
	defer bundle.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<script src="` + bundle.URL + `/assets/2-abcdef.js"></script>`))
	}))
	defer home.Close()
	a.resolver.siteURL = home.URL

	results, err := a.Search(context.Background(), "q", trackKinds())
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if searchCalls != 2 {
		t.Errorf("expected a retry after 401, got %d search calls", searchCalls)
	}
	if len(results) != 1 || results[0].Title != "OK" {
		t.Fatalf("expected retried success, got %+v", results)
	}
}

func TestSoundCloudAPIAdapter_FallsBackOnResolveFailure(t *testing.T) {
	// api-v2 base points nowhere useful; resolver homepage 500s, so client_id
	// resolution fails and the adapter must fall back.
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer home.Close()

	fb := &recordingFallback{results: []domain.SearchResult{
		{Kind: domain.ResultKindTrack, Title: "from yt-dlp"},
	}}
	a := NewSoundCloudAPIAdapter(http.DefaultClient, fb)
	a.resolver.siteURL = home.URL // no seeded client_id → forces resolution

	results, err := a.Search(context.Background(), "q", trackKinds())
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if !fb.called {
		t.Error("expected fallback to be invoked")
	}
	if len(results) != 1 || results[0].Title != "from yt-dlp" {
		t.Fatalf("expected fallback results, got %+v", results)
	}
}

func TestSoundCloudAPIAdapter_NoFallback_ReturnsError(t *testing.T) {
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer home.Close()

	a := NewSoundCloudAPIAdapter(http.DefaultClient, nil)
	a.resolver.siteURL = home.URL

	_, err := a.Search(context.Background(), "q", trackKinds())
	if err == nil {
		t.Fatal("expected error when resolution fails and no fallback set")
	}
}

func TestSoundCloudAPIAdapter_Resolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resolve" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("url") == "" {
			t.Error("expected url query param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 555,
			"kind": "track",
			"title": "Leaked Cut",
			"permalink_url": "https://soundcloud.com/x/leaked-cut",
			"user": { "username": "Artist" }
		}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	r, err := a.Resolve(context.Background(), "https://soundcloud.com/x/leaked-cut")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.Title != "Leaked Cut" || r.Sources[0].ExternalID != "555" {
		t.Fatalf("unexpected resolve result: %+v", r)
	}
}

func TestClientIDResolver_ScrapesAndCaches(t *testing.T) {
	var homeCalls, bundleCalls int
	bundle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bundleCalls++
		_, _ = w.Write([]byte(`window.__sc={a:1},client_id:"resolvedclientid0000000000000000",b:2`))
	}))
	defer bundle.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		homeCalls++
		_, _ = w.Write([]byte(
			`<script crossorigin src="` + bundle.URL + `/assets/0-aaa.js"></script>` +
				`<script crossorigin src="` + bundle.URL + `/assets/9-zzz.js"></script>`,
		))
	}))
	defer home.Close()

	r := newClientIDResolver(home.Client())
	r.siteURL = home.URL

	id, err := r.get(context.Background())
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if id != "resolvedclientid0000000000000000" {
		t.Errorf("client_id = %q", id)
	}

	// Second get must be served from cache — no further homepage hit.
	if _, err := r.get(context.Background()); err != nil {
		t.Fatalf("cached get error: %v", err)
	}
	if homeCalls != 1 {
		t.Errorf("expected homepage scraped once, got %d", homeCalls)
	}

	// After invalidate, it resolves again.
	r.invalidate()
	if _, err := r.get(context.Background()); err != nil {
		t.Fatalf("post-invalidate get error: %v", err)
	}
	if homeCalls != 2 {
		t.Errorf("expected re-scrape after invalidate, got %d homepage hits", homeCalls)
	}
}

func TestUpgradeArtworkResolution(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"large to 500", "https://i1.sndcdn.com/artworks-abc-large.jpg", "https://i1.sndcdn.com/artworks-abc-t500x500.jpg"},
		{"empty stays empty", "", ""},
		{"no large variant untouched", "https://i1.sndcdn.com/artworks-abc-t300x300.jpg", "https://i1.sndcdn.com/artworks-abc-t300x300.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upgradeArtworkResolution(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// recordingFallback is a searchFallback that records invocation and returns a
// canned result set — stands in for the yt-dlp adapter in fallback tests.
type recordingFallback struct {
	called  bool
	results []domain.SearchResult
}

func (f *recordingFallback) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	f.called = true
	return f.results, nil
}
