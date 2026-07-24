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
						"mbid": "6c9d9e5f-25cc-4e3a-9d3a-3a4b7d2f1a01",
						"url": "https://www.last.fm/music/Katy+Perry/_/Small+Talk",
						"listeners": "1234567",
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
	if r.Extras["listeners"] != "1234567" {
		t.Errorf("extras.listeners: got %v, want %q", r.Extras["listeners"], "1234567")
	}
	if r.MBID != "6c9d9e5f-25cc-4e3a-9d3a-3a4b7d2f1a01" {
		t.Errorf("MBID: got %q, want the fixture mbid", r.MBID)
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
						"mbid": "c8b03190-306c-4120-bb0b-6f2ebfc06ea9",
						"url": "https://www.last.fm/music/The+Weeknd",
						"listeners": "9876543",
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
	if r.Extras["listeners"] != "9876543" {
		t.Errorf("extras.listeners: got %v, want %q", r.Extras["listeners"], "9876543")
	}
	if r.MBID != "c8b03190-306c-4120-bb0b-6f2ebfc06ea9" {
		t.Errorf("MBID: got %q, want the fixture mbid", r.MBID)
	}
}

func TestLastFmAdapter_Search_Albums_DoesNotStampReleaseMBID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"albummatches": {
					"album": [{
						"name": "OK Computer",
						"artist": "Radiohead",
						"mbid": "0b6b4ba0-d36f-47bd-b4ea-6a5b91842d29",
						"url": "https://www.last.fm/music/Radiohead/OK+Computer",
						"image": [
							{"#text": "https://lastfm.freetls.fastly.net/album-small.png", "size": "small"},
							{"#text": "https://lastfm.freetls.fastly.net/album-xl.png", "size": "extralarge"}
						]
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "ok computer", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindAlbum {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindAlbum)
	}
	if r.Title != "OK Computer" {
		t.Errorf("title: got %q, want %q", r.Title, "OK Computer")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	// Last.fm album-search mbids are RELEASE MBIDs; MusicBrainz album results
	// carry RELEASE-GROUP MBIDs — different UUID namespaces, so stamping the
	// release mbid makes the MBID hard-stop block every MB↔Last.fm album merge.
	if r.MBID != "" {
		t.Errorf("MBID: got %q, want empty (release-namespace mbid must not be stamped)", r.MBID)
	}
}

func TestLastFmAdapter_Search_Track_MissingListeners(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"trackmatches": {
					"track": [{
						"name": "Obscure Track",
						"artist": "Unknown",
						"url": "https://www.last.fm/music/Unknown/_/Obscure+Track",
						"image": []
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "obscure", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if _, ok := results[0].Extras["listeners"]; ok {
		t.Errorf("extras should not contain 'listeners' when API omits it")
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
	// When every attempted kind fails (a single kind on HTTP 500), Search surfaces
	// an error so the circuit breaker sees the provider outage.
	if err == nil {
		t.Fatal("expected an error when all attempted kinds fail on HTTP 500, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}
