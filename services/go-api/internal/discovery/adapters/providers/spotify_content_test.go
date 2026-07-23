package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func newContentSpotifyAdapter(srv *httptest.Server) *SpotifyAdapter {
	a := NewSpotifyAdapter(srv.Client())
	a.apiBase = srv.URL
	a.resolver.cached = &spotifySession{
		accessToken:  "test-access-token",
		accessExpiry: farFuture,
		clientToken:  "test-client-token",
		clientExpiry: farFuture,
	}
	return a
}

func TestSpotifyAdapter_GetArtistAlbums(t *testing.T) {
	const albumsJSON = `{"items":[
		{"id":"al1","name":"After Hours","album_type":"album","release_date":"2020-03-20","total_tracks":14,"images":[{"url":"https://i/300.jpg","width":300},{"url":"https://i/640.jpg","width":640}],"external_urls":{"spotify":"https://open.spotify.com/album/al1"}},
		{"id":"al2","name":"Live Single","album_type":"single","release_date":"2021-01-05","total_tracks":1,"images":[{"url":"https://i/x.jpg","width":640}]}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q, want Bearer test-access-token", got)
		}
		if !strings.Contains(r.URL.Path, "/artists/artist-1/albums") {
			t.Errorf("path = %q, want the artist albums endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte(albumsJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("albums = %d, want 2", len(albums))
	}
	if albums[0].Title != "After Hours" || albums[0].ReleaseDate != "2020-03-20" || albums[0].TrackCount != 14 {
		t.Errorf("album[0] = %+v", albums[0])
	}
	if albums[0].ImageURL != "https://i/640.jpg" {
		t.Errorf("album[0].ImageURL = %q, want the widest image", albums[0].ImageURL)
	}
	if albums[1].Extras["record_type"] != "single" {
		t.Errorf("album[1] record_type = %v, want single", albums[1].Extras["record_type"])
	}
}

func TestSpotifyAdapter_GetArtistTopTracks(t *testing.T) {
	const tracksJSON = `{"tracks":[
		{"id":"t1","name":"Blinding Lights","explicit":false,"duration_ms":200000,"album":{"name":"After Hours","images":[{"url":"https://i/640.jpg","width":640}]},"artists":[{"name":"The Weeknd"}],"external_urls":{"spotify":"https://open.spotify.com/track/t1"}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/artists/artist-1/top-tracks") {
			t.Errorf("path = %q, want the top-tracks endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte(tracksJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetArtistTopTracks(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistTopTracks error = %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}
	tr := tracks[0]
	if tr.Title != "Blinding Lights" || tr.Subtitle != "The Weeknd" || tr.Album != "After Hours" || tr.Duration != 200 {
		t.Errorf("track = %+v", tr)
	}
}
