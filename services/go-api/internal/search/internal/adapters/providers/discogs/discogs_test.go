package discogs_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/artworkfallback"
	"altune/go-api/internal/search/internal/adapters/providers/discogs"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/queuetimeout"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func newTestDiscogsAdapter(server *httptest.Server) *discogs.DiscogsAdapter {
	return discogs.NewDiscogsAdapter(providertest.NewClient(server.URL), "test-token", "altune-test/1.0")
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestDiscogsAdapter_Resolve_ArtistOnly(t *testing.T) {
	t.Parallel()

	t.Run("returns empty for non-artist kind", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not make HTTP calls for non-artist kinds")
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		url, err := adapter.Resolve(context.Background(), search.ResultKindTrack, "Title", "Artist", "")
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
				writeJSON(w, `{"results": [{"id": 123, "title": "TestArtist", "type": "artist"}]}`)
			case "/artists/123":
				writeJSON(w, `{"id": 123, "name": "TestArtist", "images": [
					{"type": "secondary", "uri": "https://img.discogs.com/secondary.jpg"},
					{"type": "primary", "uri": "https://img.discogs.com/primary.jpg"}
				]}`)
			}
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)

		url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "TestArtist", "", "")
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
				writeJSON(w, `{"results": [{"id": 456, "title": "Artist2", "type": "artist"}]}`)
			case "/artists/456":
				writeJSON(w, `{"id": 456, "name": "Artist2", "images": [{"type": "secondary", "uri": "https://img.discogs.com/only.jpg"}]}`)
			}
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)

		url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "Artist2", "", "")
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
		writeJSON(w, `{"results": []}`)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "NonexistentArtist", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL for no results, got %q", url)
	}
}

func TestDiscogsAdapter_ResolveDiscogsArtist_NoResultsIsNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"results": []}`)
	}))
	defer srv.Close()

	info, err := newTestDiscogsAdapter(srv).ResolveDiscogsArtist(context.Background(), "NonexistentArtist", nil)
	if !errors.Is(err, ports.ErrDiscogsArtistNotFound) {
		t.Fatalf("error = %v, want ErrDiscogsArtistNotFound", err)
	}
	if info != nil {
		t.Errorf("info = %+v, want nil", info)
	}
}

func TestDiscogsAdapter_Resolve_429(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "Artist", "", "")
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Fatalf("expected ErrArtworkUnavailable on rate limit, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on rate limit, got %q", url)
	}
}

func TestDiscogsAdapter_ResolveDiscogsArtist_OverlapSelection(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/database/search":
			writeJSON(w, `{"results": [
				{"id": 100, "title": "Che", "type": "artist", "genre": ["Electronic", "Hip Hop"], "country": "US"},
				{"id": 200, "title": "Che Guevara", "type": "artist"}
			]}`)
		case "/artists/100/releases":
			writeJSON(w, `{"releases": [
				{"id": 1, "title": "REST IN BASS", "year": 2022},
				{"id": 2, "title": "Sayso Says", "year": 2021}
			]}`)
		case "/artists/200/releases":
			writeJSON(w, `{"releases": [{"id": 3, "title": "Revolution", "year": 1967}]}`)
		case "/artists/100":
			writeJSON(w, `{"id": 100, "name": "Che"}`)
		case "/artists/200":
			writeJSON(w, `{"id": 200, "name": "Che Guevara"}`)
		}
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

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
		writeJSON(w, `{"releases": [
			{"id": 1, "title": "Album One", "year": 2020, "type": "master"},
			{"id": 2, "title": "Album Two", "year": 2021, "type": "release"}
		]}`)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

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
		url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
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
		url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
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
		url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindAlbum,
			ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
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
		for _, id := range []ports.ArtworkIdentity{
			{},
			{ExternalIDs: map[domain.ProviderKey]string{"discogs": "not-a-number"}},
		} {
			url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist, id)
			if err != nil || url != "" {
				t.Errorf("(%q, %v), want (\"\", nil)", url, err)
			}
		}
	})

	t.Run("detail error is a classified failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		adapter := newTestDiscogsAdapter(server)
		url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
		if !errors.Is(err, ports.ErrArtworkUnavailable) || url != "" {
			t.Errorf("(%q, %v), want (\"\", ErrArtworkUnavailable) — the chain degrades", url, err)
		}
	})
}

func TestDiscogsAdapter_ArtworkSource(t *testing.T) {
	if discogs.NewDiscogsAdapter(http.DefaultClient, "t", "ua").ArtworkSource() != "discogs" {
		t.Error("ArtworkSource mismatch")
	}
}

func TestDiscogsAdapter_429LogOmitsQuery(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	adapter := newTestDiscogsAdapter(srv)

	_, _ = adapter.Resolve(context.Background(), search.ResultKindArtist, "SecretQueryText", "", "")

	out := buf.String()
	if !strings.Contains(out, "discogs.rate_limited") {
		t.Fatalf("expected rate-limit log, got %q", out)
	}
	if strings.Contains(out, "SecretQueryText") || strings.Contains(out, "?") {
		t.Errorf("log leaks query: %q", out)
	}
}

func TestDiscogsAdapter_Resolve_SearchFailureIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "Artist", "", "")
	if url != "" {
		t.Errorf("url = %q, want empty on a search failure", url)
	}
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, discovery.ErrArtworkUnavailable)", err)
	}
}

func TestDiscogsAdapter_ResolveByIdentity_DetailFailureIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
		ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
	if url != "" {
		t.Errorf("url = %q, want empty on a detail failure", url)
	}
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, discovery.ErrArtworkUnavailable)", err)
	}
}

func TestDiscogsAdapter_ResolveDiscogsArtist_DetailFailureKeepsPartialInfo(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/database/search":
			writeJSON(w, `{"results": [{"id": 77, "title": "Partial Artist", "genre": ["Rock"], "country": "UK"}]}`)
		case "/artists/77":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	info, err := adapter.ResolveDiscogsArtist(context.Background(), "Partial Artist", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil || info.ID != 77 || info.Name != "Partial Artist" {
		t.Errorf("info = %+v, want the partial search result kept", info)
	}
}

func TestDiscogsAdapter_Resolve_DetailFailureAfterAMatchIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/database/search":
			writeJSON(w, `{"results": [{"id": 123, "title": "TestArtist", "type": "artist"}]}`)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "TestArtist", "", "")
	if url != "" {
		t.Errorf("url = %q, want empty when the detail fetch fails", url)
	}
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, discovery.ErrArtworkUnavailable)", err)
	}
}

func TestDiscogsAdapter_Resolve_ArtistWithoutImagesIsAVerifiedMiss(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/database/search":
			writeJSON(w, `{"results": [{"id": 123, "title": "Imageless", "type": "artist"}]}`)
		case "/artists/123":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 123, "name": "Imageless", "images": []}`))
		}
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "Imageless", "", "")
	if err != nil || url != "" {
		t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
	}
}

func TestDiscogsAdapter_Resolve_MalformedSearchBodyIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [`))
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "Artist", "", "")
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve on a truncated body = (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestDiscogsAdapter_Resolve_TransportErrorIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	adapter := newTestDiscogsAdapter(srv)
	srv.Close()

	url, err := adapter.Resolve(context.Background(), search.ResultKindArtist, "Artist", "", "")
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve with the provider unreachable = (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestDiscogsAdapter_Resolve_CancelledContextIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"results": []}`)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	url, err := adapter.Resolve(ctx, search.ResultKindArtist, "Artist", "", "")
	if url != "" {
		t.Errorf("url = %q, want empty on a cancelled lookup", url)
	}
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, discovery.ErrArtworkUnavailable)", err)
	}
}

func TestDiscogsAdapter_ResolveByIdentity_RateLimitIsArtworkUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
		ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("ResolveByIdentity on 429 = (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestDiscogsAdapter_ResolveByIdentity_UnknownArtistIsAVerifiedMiss(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
		ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})
	if err != nil || url != "" {
		t.Errorf("ResolveByIdentity on 404 = (%q, %v), want (\"\", nil)", url, err)
	}
}

func TestDiscogsAdapter_ResolveByIdentity_ZeroDiscogsIDIsASilentMissWithNoRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP request expected for a zero discogs id")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	for _, raw := range []string{"0", "", " 38", "38abc"} {
		url, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": raw}})
		if err != nil || url != "" {
			t.Errorf("discogs id %q: (%q, %v), want (\"\", nil)", raw, url, err)
		}
	}
}

func TestDiscogsAdapter_ResolveDiscogsArtist_DetailFailureLogsTheSkippedEnrichment(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/database/search":
			writeJSON(w, `{"results": [{"id": 77, "title": "Partial Artist"}]}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)

	info, err := adapter.ResolveDiscogsArtist(context.Background(), "Partial Artist", nil)
	if err != nil || info == nil {
		t.Fatalf("ResolveDiscogsArtist = (%+v, %v), want partial info and nil error", info, err)
	}

	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "discogs.detail_enrichment_skipped") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("want a discogs.detail_enrichment_skipped log, got %q", buf.String())
	}
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("log line = %q, want level WARN", line)
	}
	if !strings.Contains(line, "error=") {
		t.Errorf("log line = %q, want an error attribute", line)
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  domain.ProviderKey
		want string
	}{
		{discogs.NewDiscogsAdapter(nil, "", "").ArtworkSource(), "discogs"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}

func inMemoryLookup() func(context.Context) error {
	client := &http.Client{Transport: providertest.FakeRoundTripper{Fn: func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id": 38, "images": []}`)),
			Request:    r,
		}, nil
	}}}
	adapter := discogs.NewDiscogsAdapter(client, "test-token", "altune-test/1.0")
	id := ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}}
	return func(ctx context.Context) error {
		_, err := adapter.ResolveByIdentity(ctx, search.ResultKindArtist, id)
		return err
	}
}

func TestDiscogsAdapter_PacesNothingItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lookup := inMemoryLookup()
		start := time.Now()

		for i := range 5 {
			if err := lookup(context.Background()); err != nil {
				t.Fatalf("lookup %d: %v", i+1, err)
			}
		}

		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("five back-to-back lookups took %v, want no wait of the adapter's own because the transport paces the host", elapsed)
		}
	})
}

func TestDiscogsAdapter_PassesATransportQueueTimeoutThroughClassified(t *testing.T) {
	shed := fmt.Errorf("%w: %w", queuetimeout.Err, context.DeadlineExceeded)
	client := &http.Client{Transport: providertest.FakeRoundTripper{Fn: func(*http.Request) (*http.Response, error) {
		return nil, shed
	}}}
	adapter := discogs.NewDiscogsAdapter(client, "test-token", "altune-test/1.0")
	id := ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}}

	_, err := adapter.ResolveByIdentity(context.Background(), search.ResultKindArtist, id)

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the shed to stay a queue timeout and a DeadlineExceeded", err)
	}
}

func TestChainedArtworkResolver_DiscogsIdentityOn500IsDegradedAndUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	discogsAdapterOn500 := newTestDiscogsAdapter(srv)

	chain := artworkfallback.NewChainedArtworkLookup(discogsAdapterOn500)
	url, _, err := chain.ResolveWithIdentityTagged(context.Background(), search.ResultKindArtist, "Artist", "",
		ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}})

	if url != "" {
		t.Errorf("url = %q, want empty when discogs fails", url)
	}
	if !errors.Is(err, ports.ErrArtworkDegraded) || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want both ErrArtworkDegraded and ErrArtworkUnavailable", err)
	}
}
