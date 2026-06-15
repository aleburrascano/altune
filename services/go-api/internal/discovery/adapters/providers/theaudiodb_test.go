package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
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
	// Resolve for album: title is album name, subtitle is artist
	url, err := adapter.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "Radiohead", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://www.theaudiodb.com/images/media/album/thumb/okcomputer.jpg" {
		t.Errorf("resolve URL: got %q, want theaudiodb album thumb", url)
	}
}
