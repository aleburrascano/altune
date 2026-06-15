package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestMusicBrainzAdapter_Search_Recordings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"recordings": [{
				"id": "abc-123-def",
				"title": "Paranoid Android",
				"isrcs": ["GBAYE9700011"],
				"artist-credit": [{"name": "Radiohead"}]
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "paranoid android", map[domain.ResultKind]bool{
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
	if r.Title != "Paranoid Android" {
		t.Errorf("title: got %q, want %q", r.Title, "Paranoid Android")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderMusicBrainz {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderMusicBrainz)
	}
	if r.Sources[0].ExternalID != "abc-123-def" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "abc-123-def")
	}
	if r.Sources[0].URL != "https://musicbrainz.org/recording/abc-123-def" {
		t.Errorf("source URL: got %q, want musicbrainz recording URL", r.Sources[0].URL)
	}
	if r.Extras["mbid"] != "abc-123-def" {
		t.Errorf("extras.mbid: got %v, want %q", r.Extras["mbid"], "abc-123-def")
	}
	if r.Extras["isrc"] != "GBAYE9700011" {
		t.Errorf("extras.isrc: got %v, want %q", r.Extras["isrc"], "GBAYE9700011")
	}
}

func TestMusicBrainzAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/artist") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"artists": [{
				"id": "a74b1b7f-71a5-4011-9441-d0b5e4122711",
				"name": "Radiohead"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "radiohead", map[domain.ResultKind]bool{
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
	if r.Title != "Radiohead" {
		t.Errorf("title: got %q, want %q", r.Title, "Radiohead")
	}
	if r.Sources[0].Provider != domain.ProviderMusicBrainz {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderMusicBrainz)
	}
	if r.Sources[0].URL != "https://musicbrainz.org/artist/a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("source URL: got %q, want musicbrainz artist URL", r.Sources[0].URL)
	}
	if r.Extras["mbid"] != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("extras.mbid: got %v, want the artist MBID", r.Extras["mbid"])
	}
}

func TestMusicBrainzAdapter_Search_ReleaseGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"release-groups": [{
				"id": "rg-001-abc",
				"title": "OK Computer",
				"artist-credit": [{"name": "Radiohead"}]
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
	if r.Sources[0].URL != "https://musicbrainz.org/release-group/rg-001-abc" {
		t.Errorf("source URL: got %q, want musicbrainz release-group URL", r.Sources[0].URL)
	}
	if r.Extras["mbid"] != "rg-001-abc" {
		t.Errorf("extras.mbid: got %v, want %q", r.Extras["mbid"], "rg-001-abc")
	}
}

func TestMusicBrainzAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
