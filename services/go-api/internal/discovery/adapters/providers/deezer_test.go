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
				"rank": 150000,
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
	if r.ISRC != "GBAYE7500101" {
		t.Errorf("ISRC: got %q, want %q", r.ISRC, "GBAYE7500101")
	}
	if r.Extras["album"] != "A Night at the Opera" {
		t.Errorf("extras.album: got %v, want %q", r.Extras["album"], "A Night at the Opera")
	}
	// JSON unmarshals numbers as float64
	if dur, ok := r.Extras["duration"].(int); !ok || dur != 355 {
		t.Errorf("extras.duration: got %v (%T), want 355", r.Extras["duration"], r.Extras["duration"])
	}
	if r.ProviderRank != 150000 {
		t.Errorf("ProviderRank: got %d, want 150000", r.ProviderRank)
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
	if r.FanCount != 5000000 {
		t.Errorf("FanCount: got %d, want 5000000", r.FanCount)
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
				"nb_tracks": 12,
				"nb_fan": 50000,
				"genre_id": 152
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
	if r.ReleaseDate != "1997-05-21" {
		t.Errorf("ReleaseDate: got %q, want %q", r.ReleaseDate, "1997-05-21")
	}
	// nb_tracks is an int in the struct, mapped to TrackCount
	if r.TrackCount != 12 {
		t.Errorf("TrackCount: got %d, want 12", r.TrackCount)
	}
	if r.FanCount != 50000 {
		t.Errorf("FanCount: got %d, want 50000", r.FanCount)
	}
	if gid, ok := r.Extras["genre_id"].(int); !ok || gid != 152 {
		t.Errorf("extras.genre_id: got %v (%T), want 152", r.Extras["genre_id"], r.Extras["genre_id"])
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
	// When every attempted kind fails (a single kind on HTTP 500), Search surfaces
	// an error so the circuit breaker sees the provider outage.
	if err == nil {
		t.Fatal("expected an error when all attempted kinds fail on HTTP 500, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

func TestDeezerAdapter_Search_Track_MissingPopularity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 999,
				"title": "Unknown Track",
				"link": "https://www.deezer.com/track/999",
				"duration": 180,
				"artist": {"id": 1, "name": "Artist"}
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "unknown", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.ProviderRank != 0 {
		t.Errorf("extras should not contain 'rank' when API returns 0")
	}
	if r.FanCount != 0 {
		t.Errorf("extras should not contain 'nb_fan' when API returns 0")
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
