package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// newContentSpotifyAdapter points the pathfinder endpoint at a test server and
// pre-seeds a valid session so content calls skip live token resolution.
func newContentSpotifyAdapter(srv *httptest.Server) *SpotifyAdapter {
	a := NewSpotifyAdapter(srv.Client())
	a.pathfinderURL = srv.URL
	a.resolver.cached = &spotifySession{
		accessToken:  "test-access-token",
		accessExpiry: farFuture,
		clientToken:  "test-client-token",
		clientExpiry: farFuture,
	}
	return a
}

// operationNameOf reads the persisted-query operationName from a pathfinder body.
func operationNameOf(t *testing.T, r *http.Request) string {
	t.Helper()
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		OperationName string `json:"operationName"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return req.OperationName
}

func TestSpotifyAdapter_GetArtistAlbums(t *testing.T) {
	// queryArtistDiscographyAll shape: all.items[].releases.items[]. Two groups:
	// an album (first of two variants — only the first is kept) and a single.
	const discographyJSON = `{"data":{"artistUnion":{"discography":{"all":{"items":[
		{"releases":{"items":[
			{"id":"al1","name":"After Hours","type":"ALBUM","coverArt":{"sources":[{"url":"https://i/300.jpg","width":300},{"url":"https://i/640.jpg","width":640}]},"date":{"isoString":"2020-03-20T00:00:00Z","year":2020},"tracks":{"totalCount":14},"sharingInfo":{"shareUrl":"https://open.spotify.com/album/al1?si=abc"}},
			{"id":"al1b","name":"After Hours (Deluxe)","type":"ALBUM","date":{"isoString":"2020-03-21T00:00:00Z","year":2020},"tracks":{"totalCount":16}}
		]}},
		{"releases":{"items":[
			{"id":"s1","name":"Live Single","type":"SINGLE","coverArt":{"sources":[{"url":"https://i/x.jpg","width":640}]},"date":{"isoString":"2021-01-05T00:00:00Z","year":2021},"tracks":{"totalCount":1}}
		]}}
	]}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q, want Bearer test-access-token", got)
		}
		if op := operationNameOf(t, r); op != "queryArtistDiscographyAll" {
			t.Errorf("operationName = %q, want queryArtistDiscographyAll", op)
		}
		_, _ = w.Write([]byte(discographyJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("albums = %d, want 2 (one representative per group)", len(albums))
	}
	if albums[0].Title != "After Hours" || albums[0].ReleaseDate != "2020-03-20" || albums[0].TrackCount != 14 {
		t.Errorf("album[0] = %+v", albums[0])
	}
	if albums[0].ImageURL != "https://i/640.jpg" {
		t.Errorf("album[0].ImageURL = %q, want the widest image", albums[0].ImageURL)
	}
	if albums[0].Sources[0].URL != "https://open.spotify.com/album/al1" {
		t.Errorf("album[0] URL = %q, want the share URL stripped of ?si=", albums[0].Sources[0].URL)
	}
	if albums[1].Extras["record_type"] != "single" {
		t.Errorf("album[1] record_type = %v, want single", albums[1].Extras["record_type"])
	}
}

func TestSpotifyAdapter_GetArtistTopTracks(t *testing.T) {
	const overviewJSON = `{"data":{"artistUnion":{"discography":{"topTracks":{"items":[
		{"track":{"id":"t1","name":"Blinding Lights","contentRating":{"label":"EXPLICIT"},"duration":{"totalMilliseconds":200000},"albumOfTrack":{"coverArt":{"sources":[{"url":"https://i/640.jpg","width":640}]}},"artists":{"items":[{"profile":{"name":"The Weeknd"}}]}}}
	]}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if op := operationNameOf(t, r); op != "queryArtistOverview" {
			t.Errorf("operationName = %q, want queryArtistOverview", op)
		}
		_, _ = w.Write([]byte(overviewJSON))
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
	if tr.Title != "Blinding Lights" || tr.Subtitle != "The Weeknd" || tr.Duration != 200 {
		t.Errorf("track = %+v", tr)
	}
	if tr.Extras["explicit"] != true {
		t.Errorf("track explicit = %v, want true", tr.Extras["explicit"])
	}
}

func TestSpotifyAdapter_Content_surfacesGraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"PersistedQueryNotFound"}],"data":null}`))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	_, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err == nil || !strings.Contains(err.Error(), "PersistedQueryNotFound") {
		t.Fatalf("err = %v, want a surfaced GraphQL error", err)
	}
}
