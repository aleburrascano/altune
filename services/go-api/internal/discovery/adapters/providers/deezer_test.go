package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// redirectTransport rewrites all outgoing request URLs to the test server.
type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	target := strings.TrimPrefix(t.targetURL, "http://")
	req.URL.Host = target
	return http.DefaultTransport.RoundTrip(req)
}

func newTestClient(serverURL string) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{targetURL: serverURL},
	}
}

func TestDeezerAdapter_Search_Tracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/track") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 123456,
				"title": "Bohemian Rhapsody",
				"link": "https://www.deezer.com/track/123456",
				"duration": 355,
				"isrc": "GBAYE7500101",
				"artist": {"id": 1, "name": "Queen"},
				"album": {"id": 10, "title": "A Night at the Opera", "cover_big": "https://cdn.deezer.com/cover.jpg"}
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "bohemian rhapsody", map[domain.ResultKind]bool{
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
	if r.Title != "Bohemian Rhapsody" {
		t.Errorf("title: got %q, want %q", r.Title, "Bohemian Rhapsody")
	}
	if r.Subtitle != "Queen" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Queen")
	}
	if r.ImageURL != "https://cdn.deezer.com/cover.jpg" {
		t.Errorf("imageURL: got %q, want album cover_big", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderDeezer {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderDeezer)
	}
	if r.Sources[0].ExternalID != "123456" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "123456")
	}
	if r.Extras["isrc"] != "GBAYE7500101" {
		t.Errorf("extras.isrc: got %v, want %q", r.Extras["isrc"], "GBAYE7500101")
	}
	if r.Extras["album"] != "A Night at the Opera" {
		t.Errorf("extras.album: got %v, want %q", r.Extras["album"], "A Night at the Opera")
	}
	// JSON unmarshals numbers as float64
	if dur, ok := r.Extras["duration"].(int); !ok || dur != 355 {
		t.Errorf("extras.duration: got %v (%T), want 355", r.Extras["duration"], r.Extras["duration"])
	}
}

func TestDeezerAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/artist") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 42,
				"name": "Radiohead",
				"link": "https://www.deezer.com/artist/42",
				"picture_big": "https://cdn.deezer.com/artist.jpg",
				"nb_fan": 5000000
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "radiohead", map[domain.ResultKind]bool{
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
	if r.Title != "Radiohead" {
		t.Errorf("title: got %q, want %q", r.Title, "Radiohead")
	}
	if r.ImageURL != "https://cdn.deezer.com/artist.jpg" {
		t.Errorf("imageURL: got %q, want picture_big", r.ImageURL)
	}
	nbFan, ok := r.Extras["nb_fan"].(int64)
	if !ok || nbFan != 5000000 {
		t.Errorf("extras.nb_fan: got %v (%T), want 5000000", r.Extras["nb_fan"], r.Extras["nb_fan"])
	}
}

func TestDeezerAdapter_Search_Albums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/album") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 99,
				"title": "OK Computer",
				"link": "https://www.deezer.com/album/99",
				"cover_big": "https://cdn.deezer.com/album.jpg",
				"artist": {"id": 42, "name": "Radiohead"},
				"record_type": "album",
				"release_date": "1997-05-21",
				"nb_tracks": 12
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "ok computer", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
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
	if r.ImageURL != "https://cdn.deezer.com/album.jpg" {
		t.Errorf("imageURL: got %q, want cover_big", r.ImageURL)
	}
	if r.Extras["record_type"] != "album" {
		t.Errorf("extras.record_type: got %v, want %q", r.Extras["record_type"], "album")
	}
	if r.Extras["release_date"] != "1997-05-21" {
		t.Errorf("extras.release_date: got %v, want %q", r.Extras["release_date"], "1997-05-21")
	}
	// nb_tracks is an int in the struct, mapped to extras["track_count"]
	if tc, ok := r.Extras["track_count"].(int); !ok || tc != 12 {
		t.Errorf("extras.track_count: got %v (%T), want 12", r.Extras["track_count"], r.Extras["track_count"])
	}
}

func TestDeezerAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	// The Search method silently continues on non-200, returning empty results and nil error
	if err != nil {
		t.Fatalf("expected nil error on HTTP 500 (silent skip), got: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

func TestDeezerAdapter_GetAlbumTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/album/99/tracks") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [
				{
					"id": 201,
					"title": "Airbag",
					"link": "https://www.deezer.com/track/201",
					"duration": 284,
					"artist": {"id": 42, "name": "Radiohead"}
				},
				{
					"id": 202,
					"title": "Paranoid Android",
					"link": "https://www.deezer.com/track/202",
					"duration": 383,
					"artist": {"id": 42, "name": "Radiohead"}
				}
			]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.GetAlbumTracks(context.Background(), domain.ProviderDeezer, "99")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(results))
	}
	if results[0].Title != "Airbag" {
		t.Errorf("first track title: got %q, want %q", results[0].Title, "Airbag")
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Errorf("first track kind: got %v, want %v", results[0].Kind, domain.ResultKindTrack)
	}
	if results[1].Title != "Paranoid Android" {
		t.Errorf("second track title: got %q, want %q", results[1].Title, "Paranoid Android")
	}
}

func TestDeezerAdapter_GetArtistTopTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/artist/42/top") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 301,
				"title": "Creep",
				"link": "https://www.deezer.com/track/301",
				"duration": 236,
				"artist": {"id": 42, "name": "Radiohead"}
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.GetArtistTopTracks(context.Background(), domain.ProviderDeezer, "42")
	if err != nil {
		t.Fatalf("GetArtistTopTracks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 track, got %d", len(results))
	}
	if results[0].Title != "Creep" {
		t.Errorf("track title: got %q, want %q", results[0].Title, "Creep")
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Errorf("track kind: got %v, want %v", results[0].Kind, domain.ResultKindTrack)
	}
	if results[0].Sources[0].ExternalID != "301" {
		t.Errorf("source externalID: got %q, want %q", results[0].Sources[0].ExternalID, "301")
	}
}
