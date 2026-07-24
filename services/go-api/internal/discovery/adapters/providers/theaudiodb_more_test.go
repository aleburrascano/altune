package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestTheAudioDBAdapter_Resolve_ArtistByMBID(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "artist-mb.php") {
			_, _ = w.Write([]byte(`{"artists": [{"strArtistThumb": "https://img/mbid-thumb.jpg"}]}`))
			return
		}
		t.Errorf("unexpected fallback to %q when the MBID lookup succeeds", r.URL.Path)
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Che", "", "mbid-che-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if url != "https://img/mbid-thumb.jpg" {
		t.Errorf("url = %q, want the identity-keyed MBID thumb", url)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "artist-mb.php") {
		t.Errorf("paths = %v, want the single artist-mb.php lookup", paths)
	}
}

func TestTheAudioDBAdapter_Resolve_MBIDMissFallsBackToNameSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "artist-mb.php") {
			_, _ = w.Write([]byte(`{"artists": null}`)) // MBID unknown to TADB
			return
		}
		_, _ = w.Write([]byte(`{"artists": [{"idArtist": "1", "strArtist": "Che", "strArtistThumb": "https://img/name-thumb.jpg"}]}`))
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Che", "", "mbid-unknown")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if url != "https://img/name-thumb.jpg" {
		t.Errorf("url = %q, want the name-search fallback thumb", url)
	}
}

func TestTheAudioDBAdapter_Resolve_ArtistSearchErrorIsEmptyNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Che", "", "")
	if err != nil || url != "" {
		t.Errorf("Resolve on 500 = (%q, %v), want (\"\", nil) — the artwork chain degrades", url, err)
	}
}

func TestTheAudioDBAdapter_Resolve_AlbumWithoutSubtitleIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request expected without an artist subtitle")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	url, err := adapter.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "", "")
	if err != nil || url != "" {
		t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
	}
}

// Search's documented policy: transport/status errors PROPAGATE (unlike
// Resolve, which degrades) so the fan-out's circuit breaker sees the outage.
func TestTheAudioDBAdapter_Search_http500Propagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	_, err := adapter.Search(context.Background(), "che", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err == nil {
		t.Fatal("expected the HTTP 500 to propagate from Search")
	}
}

func TestTheAudioDBAdapter_meta(t *testing.T) {
	adapter := NewTheAudioDBAdapter(http.DefaultClient)
	if adapter.Name() != domain.ProviderTheAudioDB {
		t.Errorf("Name = %v", adapter.Name())
	}
	kinds := adapter.SupportedKinds()
	if !kinds[domain.ResultKindArtist] || kinds[domain.ResultKindTrack] || kinds[domain.ResultKindAlbum] {
		t.Errorf("SupportedKinds = %v, want artist-only", kinds)
	}
	if adapter.ArtworkSource() != "theaudiodb" {
		t.Errorf("ArtworkSource = %q", adapter.ArtworkSource())
	}
}
