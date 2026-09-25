package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestDiscogsAdapter(server *httptest.Server) *DiscogsAdapter {
	return &DiscogsAdapter{
		client:    server.Client(),
		token:     "test-token",
		userAgent: "altune-test/1.0",
		limiter:   newMinIntervalLimiter(time.Second),
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
			switch r.URL.Path {
			case "/database/search":
				json.NewEncoder(w).Encode(discogsSearchResponse{
					Results: []discogsSearchResult{{ID: 123, Title: "TestArtist", Type: "artist"}},
				})
			case "/artists/123":
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
			switch r.URL.Path {
			case "/database/search":
				json.NewEncoder(w).Encode(discogsSearchResponse{
					Results: []discogsSearchResult{{ID: 456, Title: "Artist2", Type: "artist"}},
				})
			case "/artists/456":
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
		switch r.URL.Path {
		case "/database/search":
			json.NewEncoder(w).Encode(discogsSearchResponse{
				Results: []discogsSearchResult{
					{ID: 100, Title: "Che", Type: "artist", Genre: []string{"Electronic", "Hip Hop"}, Country: "US"},
					{ID: 200, Title: "Che Guevara", Type: "artist"},
				},
			})
		case "/artists/100/releases":
			json.NewEncoder(w).Encode(discogsReleasesResponse{
				Releases: []discogsRelease{
					{ID: 1, Title: "REST IN BASS", Year: 2022},
					{ID: 2, Title: "Sayso Says", Year: 2021},
				},
			})
		case "/artists/200/releases":
			json.NewEncoder(w).Encode(discogsReleasesResponse{
				Releases: []discogsRelease{
					{ID: 3, Title: "Revolution", Year: 1967},
				},
			})
		case "/artists/100":
			callCount++
			json.NewEncoder(w).Encode(discogsArtistDetail{ID: 100, Name: "Che"})
		case "/artists/200":
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
	if info.Genre != "Electronic, Hip Hop" {
		t.Errorf("expected genre %q, got %q", "Electronic, Hip Hop", info.Genre)
	}
	if info.Country != "US" {
		t.Errorf("expected country %q, got %q", "US", info.Country)
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

func TestDiscogsAdapter_ResolveByIdentity(t *testing.T) {
	t.Run("primary image of the bridged artist", func(t *testing.T) {
		var gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 38, "name": "Che (38)", "images": [
				{"type": "secondary", "uri": "https://img/secondary.jpg"},
				{"type": "primary", "uri": "https://img/primary.jpg"}
			]}`))
		}))
		defer server.Close()

		adapter := newTestDiscogsAdapter(server)
		overrideDiscogsBaseURL(adapter, server.URL)
		url, err := adapter.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"discogs": "38"}})
		if err != nil {
			t.Fatalf("ResolveByIdentity: %v", err)
		}
		if url != "https://img/primary.jpg" {
			t.Errorf("url = %q, want the primary image preferred", url)
		}
		if gotPath != "/artists/38" {
			t.Errorf("path = %q, want the exact bridged id — no name search", gotPath)
		}
	})

	t.Run("falls back to first image without a primary", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 38, "images": [{"type": "secondary", "uri": "https://img/only.jpg"}]}`))
		}))
		defer server.Close()

		adapter := newTestDiscogsAdapter(server)
		overrideDiscogsBaseURL(adapter, server.URL)
		url, err := adapter.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"discogs": "38"}})
		if err != nil || url != "https://img/only.jpg" {
			t.Errorf("(%q, %v), want the first image", url, err)
		}
	})

	t.Run("non-artist kind is a silent miss", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected for a non-artist kind")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		adapter := newTestDiscogsAdapter(server)
		overrideDiscogsBaseURL(adapter, server.URL)
		url, err := adapter.ResolveByIdentity(context.Background(), domain.ResultKindAlbum,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"discogs": "38"}})
		if err != nil || url != "" {
			t.Errorf("(%q, %v), want (\"\", nil)", url, err)
		}
	})

	t.Run("missing or non-numeric discogs id is a silent miss", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected without a usable discogs id")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		adapter := newTestDiscogsAdapter(server)
		overrideDiscogsBaseURL(adapter, server.URL)
		for _, id := range []ports.ArtworkIdentity{
			{},
			{ExternalIDs: map[string]string{"discogs": "not-a-number"}},
		} {
			url, err := adapter.ResolveByIdentity(context.Background(), domain.ResultKindArtist, id)
			if err != nil || url != "" {
				t.Errorf("(%q, %v), want (\"\", nil)", url, err)
			}
		}
	})

	t.Run("detail error is a silent miss", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		adapter := newTestDiscogsAdapter(server)
		overrideDiscogsBaseURL(adapter, server.URL)
		url, err := adapter.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"discogs": "38"}})
		if err != nil || url != "" {
			t.Errorf("(%q, %v), want (\"\", nil) — the chain degrades", url, err)
		}
	})
}

func TestDiscogsAdapter_rateLimit_spacesConsecutiveCalls(t *testing.T) {
	a := NewDiscogsAdapter(http.DefaultClient, "tok", "ua")

	start := time.Now()
	_ = a.limiter.wait(context.Background())
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("first call blocked %v, want immediate", elapsed)
	}

	start = time.Now()
	_ = a.limiter.wait(context.Background())
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Errorf("second call blocked only %v, want ~1s spacing", elapsed)
	}
}

func TestDiscogsAdapter_ArtworkSource(t *testing.T) {
	if NewDiscogsAdapter(http.DefaultClient, "t", "ua").ArtworkSource() != "discogs" {
		t.Error("ArtworkSource mismatch")
	}
}

func TestDiscogsAdapter_429LogOmitsQuery(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()
	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	_, _ = adapter.Resolve(context.Background(), domain.ResultKindArtist, "SecretQueryText", "", "")

	out := buf.String()
	if !strings.Contains(out, "discogs.rate_limited") {
		t.Fatalf("expected rate-limit log, got %q", out)
	}
	if strings.Contains(out, "SecretQueryText") || strings.Contains(out, "?") {
		t.Errorf("log leaks query: %q", out)
	}
}
