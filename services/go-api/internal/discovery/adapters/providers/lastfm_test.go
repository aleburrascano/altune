package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestLastFmAdapter_Search_Tracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"trackmatches": {
					"track": [{
						"name": "Small Talk",
						"artist": "Katy Perry",
						"url": "https://www.last.fm/music/Katy+Perry/_/Small+Talk",
						"image": [
							{"#text": "https://lastfm.freetls.fastly.net/small.png", "size": "small"},
							{"#text": "https://lastfm.freetls.fastly.net/extralarge.png", "size": "extralarge"}
						]
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "small talk", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Small Talk" {
		t.Errorf("title: got %q, want %q", r.Title, "Small Talk")
	}
	if r.Subtitle != "Katy Perry" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Katy Perry")
	}
	if r.ImageURL != "https://lastfm.freetls.fastly.net/extralarge.png" {
		t.Errorf("imageURL: got %q, want extralarge image URL", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderLastFM {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderLastFM)
	}
	// lastfmExternalID extracts path after /music/
	if r.Sources[0].ExternalID != "Katy+Perry/_/Small+Talk" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "Katy+Perry/_/Small+Talk")
	}
	if r.Sources[0].URL != "https://www.last.fm/music/Katy+Perry/_/Small+Talk" {
		t.Errorf("source URL: got %q, want last.fm track URL", r.Sources[0].URL)
	}
}

func TestLastFmAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"artistmatches": {
					"artist": [{
						"name": "The Weeknd",
						"url": "https://www.last.fm/music/The+Weeknd",
						"image": [
							{"#text": "https://lastfm.freetls.fastly.net/artist-small.png", "size": "small"},
							{"#text": "https://lastfm.freetls.fastly.net/artist-xl.png", "size": "extralarge"}
						]
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "the weeknd", map[domain.ResultKind]bool{
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
	if r.Title != "The Weeknd" {
		t.Errorf("title: got %q, want %q", r.Title, "The Weeknd")
	}
	if r.ImageURL != "https://lastfm.freetls.fastly.net/artist-xl.png" {
		t.Errorf("imageURL: got %q, want extralarge artist image", r.ImageURL)
	}
	if r.Sources[0].ExternalID != "The+Weeknd" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "The+Weeknd")
	}
}

func TestLastFmAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("expected nil error on HTTP 500 (silent skip), got: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}
