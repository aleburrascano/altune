package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestITunesAdapter_Search_Tracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [{
				"trackId": 456789,
				"trackName": "Stairway to Heaven",
				"artistName": "Led Zeppelin",
				"collectionName": "Led Zeppelin IV",
				"trackViewUrl": "https://music.apple.com/track/456789",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/image/100x100.jpg",
				"trackTimeMillis": 482000,
				"primaryGenreName": "Rock"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "stairway to heaven", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Stairway to Heaven" {
		t.Errorf("title: got %q, want %q", r.Title, "Stairway to Heaven")
	}
	if r.Subtitle != "Led Zeppelin" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Led Zeppelin")
	}
	// artwork URL should be upscaled from 100x100 to 600x600
	if !strings.Contains(r.ImageURL, "600x600") {
		t.Errorf("imageURL should contain 600x600, got %q", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderITunes {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderITunes)
	}
	if r.Sources[0].ExternalID != "456789" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "456789")
	}
	if r.Sources[0].URL != "https://music.apple.com/track/456789" {
		t.Errorf("source URL: got %q, want apple music URL", r.Sources[0].URL)
	}
	if r.Extras["album"] != "Led Zeppelin IV" {
		t.Errorf("extras.album: got %v, want %q", r.Extras["album"], "Led Zeppelin IV")
	}
	// duration should be trackTimeMillis/1000 = 482
	if dur, ok := r.Extras["duration"].(int64); !ok || dur != 482 {
		t.Errorf("extras.duration: got %v (%T), want 482", r.Extras["duration"], r.Extras["duration"])
	}
	if r.Extras["genre"] != "Rock" {
		t.Errorf("extras.genre: got %v, want %q", r.Extras["genre"], "Rock")
	}
}

func TestITunesAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [{
				"trackId": 0,
				"trackName": "",
				"artistName": "Pink Floyd",
				"collectionName": "",
				"trackViewUrl": "https://music.apple.com/artist/pinkfloyd",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/artist/100x100.jpg",
				"trackTimeMillis": 0
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "pink floyd", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "Pink Floyd" {
		t.Errorf("title: got %q, want %q", r.Title, "Pink Floyd")
	}
}

func TestITunesAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	// iTunes adapter silently skips non-200, returns empty results
	if err != nil {
		t.Fatalf("expected nil error on HTTP 500 (silent skip), got: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}
