package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheAudioDBAdapter_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "search.php") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"artists": [{
				"idArtist": "111239",
				"strArtist": "Coldplay",
				"strArtistThumb": "https://www.theaudiodb.com/images/media/artist/thumb/coldplay.jpg"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "coldplay", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "Coldplay" {
		t.Errorf("title: got %q, want %q", r.Title, "Coldplay")
	}
	if r.ImageURL != "https://www.theaudiodb.com/images/media/artist/thumb/coldplay.jpg" {
		t.Errorf("imageURL: got %q, want theaudiodb artist thumb URL", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderTheAudioDB {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderTheAudioDB)
	}
	if r.Sources[0].ExternalID != "111239" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "111239")
	}
}

func TestTheAudioDBAdapter_Search_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"artists": null}`))
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "nonexistent artist xyz", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for null artists, got %d", len(results))
	}
}

func TestTheAudioDBAdapter_Search_UnsupportedKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for unsupported kind")
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for unsupported kind, got %d", len(results))
	}
}

func TestTheAudioDBAdapter_Resolve_Artist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"artists": [{
				"idArtist": "111239",
				"strArtist": "Coldplay",
				"strArtistThumb": "https://www.theaudiodb.com/images/media/artist/thumb/coldplay.jpg"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Coldplay", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://www.theaudiodb.com/images/media/artist/thumb/coldplay.jpg" {
		t.Errorf("resolve URL: got %q, want theaudiodb artist thumb", url)
	}
}

func TestTheAudioDBAdapter_Resolve_Album(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "searchalbum.php") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"album": [{
				"strAlbumThumb": "https://www.theaudiodb.com/images/media/album/thumb/okcomputer.jpg"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	url, err := adapter.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "Radiohead", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://www.theaudiodb.com/images/media/album/thumb/okcomputer.jpg" {
		t.Errorf("resolve URL: got %q, want theaudiodb album thumb", url)
	}
}

func TestTheAudioDBAdapter_Search_transportErrorSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err == nil {
		t.Fatal("expected an error on HTTP 500, got nil (dead provider indistinguishable from no-matches)")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

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
			_, _ = w.Write([]byte(`{"artists": null}`))
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
