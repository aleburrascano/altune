package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func newTestDiscogsAdapter(server *httptest.Server) *DiscogsAdapter {
	return &DiscogsAdapter{
		client:    server.Client(),
		token:     "test-token",
		userAgent: "altune-test/1.0",
	}
}

func TestDiscogsAdapter_Resolve_ArtistOnly(t *testing.T) {
	t.Parallel()

	t.Run("returns empty for non-artist kind", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not make HTTP calls for non-artist kinds")
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		url, err := adapter.Resolve(context.Background(), domain.ResultKindTrack, "Title", "Artist", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "" {
			t.Errorf("expected empty URL for track kind, got %q", url)
		}
	})

	t.Run("returns primary image", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/database/search":
				json.NewEncoder(w).Encode(discogsSearchResponse{
					Results: []discogsSearchResult{{ID: 123, Title: "TestArtist", Type: "artist"}},
				})
			case r.URL.Path == "/artists/123":
				json.NewEncoder(w).Encode(discogsArtistDetail{
					ID:   123,
					Name: "TestArtist",
					Images: []discogsImage{
						{Type: "secondary", URI: "https://img.discogs.com/secondary.jpg"},
						{Type: "primary", URI: "https://img.discogs.com/primary.jpg"},
					},
				})
			}
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		adapter.client = srv.Client()
		overrideDiscogsBaseURL(adapter, srv.URL)

		url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "TestArtist", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://img.discogs.com/primary.jpg" {
			t.Errorf("expected primary image URL, got %q", url)
		}
	})

	t.Run("falls back to first image when no primary", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/database/search":
				json.NewEncoder(w).Encode(discogsSearchResponse{
					Results: []discogsSearchResult{{ID: 456, Title: "Artist2", Type: "artist"}},
				})
			case r.URL.Path == "/artists/456":
				json.NewEncoder(w).Encode(discogsArtistDetail{
					ID:     456,
					Name:   "Artist2",
					Images: []discogsImage{{Type: "secondary", URI: "https://img.discogs.com/only.jpg"}},
				})
			}
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Artist2", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://img.discogs.com/only.jpg" {
			t.Errorf("expected fallback image URL, got %q", url)
		}
	})
}

func TestDiscogsAdapter_Resolve_NoResults(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(discogsSearchResponse{Results: []discogsSearchResult{}})
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "NonexistentArtist", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL for no results, got %q", url)
	}
}

func TestDiscogsAdapter_Resolve_429(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Artist", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on rate limit, got %q", url)
	}
}

func TestDiscogsAdapter_ResolveDiscogsArtist_OverlapSelection(t *testing.T) {
	t.Parallel()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/database/search":
			json.NewEncoder(w).Encode(discogsSearchResponse{
				Results: []discogsSearchResult{
					{ID: 100, Title: "Che", Type: "artist"},
					{ID: 200, Title: "Che Guevara", Type: "artist"},
				},
			})
		case r.URL.Path == "/artists/100/releases":
			json.NewEncoder(w).Encode(discogsReleasesResponse{
				Releases: []discogsRelease{
					{ID: 1, Title: "REST IN BASS", Year: 2022},
					{ID: 2, Title: "Sayso Says", Year: 2021},
				},
			})
		case r.URL.Path == "/artists/200/releases":
			json.NewEncoder(w).Encode(discogsReleasesResponse{
				Releases: []discogsRelease{
					{ID: 3, Title: "Revolution", Year: 1967},
				},
			})
		case r.URL.Path == "/artists/100":
			callCount++
			json.NewEncoder(w).Encode(discogsArtistDetail{ID: 100, Name: "Che"})
		case r.URL.Path == "/artists/200":
			callCount++
			json.NewEncoder(w).Encode(discogsArtistDetail{ID: 200, Name: "Che Guevara"})
		}
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	info, err := adapter.ResolveDiscogsArtist(context.Background(), "Che", []string{"REST IN BASS", "Sayso Says"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.ID != 100 {
		t.Errorf("expected artist ID 100 (highest overlap), got %d", info.ID)
	}
	if info.Overlap != 2 {
		t.Errorf("expected overlap 2, got %d", info.Overlap)
	}
}

func TestDiscogsAdapter_FetchArtistReleases(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(discogsReleasesResponse{
			Releases: []discogsRelease{
				{ID: 1, Title: "Album One", Year: 2020, Type: "master"},
				{ID: 2, Title: "Album Two", Year: 2021, Type: "release"},
			},
		})
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	releases, err := adapter.FetchArtistReleases(context.Background(), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "Album One" {
		t.Errorf("expected first release 'Album One', got %q", releases[0].Title)
	}
	if releases[1].Year != 2021 {
		t.Errorf("expected second release year 2021, got %d", releases[1].Year)
	}
}

// overrideDiscogsBaseURL is a test helper that patches the adapter's HTTP client
// transport to redirect Discogs API calls to the test server.
func overrideDiscogsBaseURL(adapter *DiscogsAdapter, baseURL string) {
	adapter.client = &http.Client{
		Transport: &rewriteTransport{base: baseURL},
	}
}

type rewriteTransport struct {
	base string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req)
}
