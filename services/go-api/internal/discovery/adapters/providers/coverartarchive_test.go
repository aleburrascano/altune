package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// newNoFollowTestClient redirects requests to the test server WITHOUT following
// redirects, so the resolver's own 30x handling is what's under test.
func newNoFollowTestClient(serverURL string) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{targetURL: serverURL},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestCoverArtArchiveResolver_Resolve(t *testing.T) {
	t.Run("empty mbid is a silent miss with no request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected without an mbid")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		r := NewCoverArtArchiveResolver(newTestClient(server.URL))
		url, err := r.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "Radiohead", "")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
		}
	})

	t.Run("artist kind never resolves (CAA is release art)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected for an artist kind")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		r := NewCoverArtArchiveResolver(newTestClient(server.URL))
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "mbid-1")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
		}
	})

	t.Run("redirect returns the CDN Location", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead {
				t.Errorf("method = %q, want HEAD (no body download)", r.Method)
			}
			if !strings.Contains(r.URL.Path, "/release-group/rg-1/front-1200") {
				t.Errorf("path = %q, want the front-1200 hero tier", r.URL.Path)
			}
			w.Header().Set("Location", "https://archive.org/img/front-1200.jpg")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}))
		defer server.Close()

		r := NewCoverArtArchiveResolver(newNoFollowTestClient(server.URL))
		url, err := r.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "Radiohead", "rg-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if url != "https://archive.org/img/front-1200.jpg" {
			t.Errorf("url = %q, want the redirect Location", url)
		}
	})

	t.Run("200 returns the canonical url", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		r := NewCoverArtArchiveResolver(newNoFollowTestClient(server.URL))
		url, err := r.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "Radiohead", "rg-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
			t.Errorf("url = %q, want the canonical front-1200 URL", url)
		}
	})

	t.Run("404 is a clean miss", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		r := NewCoverArtArchiveResolver(newNoFollowTestClient(server.URL))
		url, err := r.Resolve(context.Background(), domain.ResultKindAlbum, "X", "Y", "rg-miss")
		if err != nil || url != "" {
			t.Errorf("Resolve on 404 = (%q, %v), want (\"\", nil)", url, err)
		}
	})

	t.Run("unexpected status is a silent miss", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		r := NewCoverArtArchiveResolver(newNoFollowTestClient(server.URL))
		url, err := r.Resolve(context.Background(), domain.ResultKindAlbum, "X", "Y", "rg-1")
		if err != nil || url != "" {
			t.Errorf("Resolve on 500 = (%q, %v), want (\"\", nil) — the chain degrades", url, err)
		}
	})
}

func TestCoverArtArchiveResolver_ArtworkSource(t *testing.T) {
	if (&CoverArtArchiveResolver{}).ArtworkSource() != "coverartarchive" {
		t.Error("ArtworkSource mismatch")
	}
}
