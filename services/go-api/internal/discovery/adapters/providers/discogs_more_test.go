package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

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

// The Discogs limiter spaces consecutive requests ~1s apart (its published
// per-token budget). First call is free; the second blocks for the remainder.
func TestDiscogsAdapter_rateLimit_spacesConsecutiveCalls(t *testing.T) {
	a := NewDiscogsAdapter(http.DefaultClient, "tok", "ua")

	start := time.Now()
	a.rateLimit() // first call: lastReq zero → immediate
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("first call blocked %v, want immediate", elapsed)
	}

	start = time.Now()
	a.rateLimit() // second call: must wait out the 1s interval
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Errorf("second call blocked only %v, want ~1s spacing", elapsed)
	}
}

func TestDiscogsAdapter_ArtworkSource(t *testing.T) {
	if NewDiscogsAdapter(http.DefaultClient, "t", "ua").ArtworkSource() != "discogs" {
		t.Error("ArtworkSource mismatch")
	}
}
