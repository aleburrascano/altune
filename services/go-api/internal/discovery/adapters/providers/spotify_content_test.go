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

func TestSpotifyAdapter_GetAlbumTracks(t *testing.T) {
	const tracksJSON = `{"data":{"albumUnion":{"tracksV2":{"items":[
		{"track":{"uri":"spotify:track:tr1","name":"Like Lil Mexico","trackNumber":1,"contentRating":{"label":"EXPLICIT"},"duration":{"totalMilliseconds":176830},"artists":{"items":[{"profile":{"name":"Che"}}]}}},
		{"track":{"uri":"spotify:track:tr2","name":"Second","trackNumber":2,"duration":{"totalMilliseconds":120000},"artists":{"items":[{"profile":{"name":"Che"}}]}}}
	]}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if op := operationNameOf(t, r); op != "queryAlbumTracks" {
			t.Errorf("operationName = %q, want queryAlbumTracks", op)
		}
		_, _ = w.Write([]byte(tracksJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderSpotify, "album-1")
	if err != nil {
		t.Fatalf("GetAlbumTracks error = %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks = %d, want 2 (album order preserved)", len(tracks))
	}
	if tracks[0].Title != "Like Lil Mexico" || tracks[0].Subtitle != "Che" || tracks[0].Duration != 176 {
		t.Errorf("track[0] = %+v", tracks[0])
	}
	if tracks[0].Sources[0].ExternalID != "tr1" {
		t.Errorf("track[0] id = %q, want tr1 (extracted from uri)", tracks[0].Sources[0].ExternalID)
	}
	if tracks[0].Extras["explicit"] != true {
		t.Errorf("track[0] explicit = %v, want true", tracks[0].Extras["explicit"])
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

// offsetOf reads the pathfinder variables.offset from a request body.
func offsetOf(t *testing.T, raw []byte) int {
	t.Helper()
	var req struct {
		Variables struct {
			Offset int `json:"offset"`
		} `json:"variables"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return req.Variables.Offset
}

func TestSpotifyAdapter_GetArtistAlbums_paginates(t *testing.T) {
	// totalCount says 3 groups; page 1 (offset 0) carries 2, page 2 (offset 50)
	// carries the last, so the loop must stop after two requests.
	const page1 = `{"data":{"artistUnion":{"discography":{"all":{"totalCount":3,"items":[
		{"releases":{"items":[{"id":"al1","name":"First","type":"ALBUM"}]}},
		{"releases":{"items":[{"id":"al2","name":"Second","type":"ALBUM"}]}}
	]}}}}}`
	const page2 = `{"data":{"artistUnion":{"discography":{"all":{"totalCount":3,"items":[
		{"releases":{"items":[{"id":"al3","name":"Third","type":"SINGLE"}]}}
	]}}}}}`
	var offsets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		offset := offsetOf(t, raw)
		offsets = append(offsets, offset)
		if offset == 0 {
			_, _ = w.Write([]byte(page1))
			return
		}
		_, _ = w.Write([]byte(page2))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != 50 {
		t.Errorf("offsets = %v, want [0 50]", offsets)
	}
	if len(albums) != 3 {
		t.Fatalf("albums = %d, want 3 across pages", len(albums))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if albums[i].Title != want {
			t.Errorf("album[%d] = %q, want %q (pages appended in request order)", i, albums[i].Title, want)
		}
	}
}

func TestSpotifyAdapter_GetAlbumTracks_paginates(t *testing.T) {
	const page1 = `{"data":{"albumUnion":{"tracksV2":{"totalCount":3,"items":[
		{"track":{"uri":"spotify:track:tr1","name":"One","trackNumber":1}},
		{"track":{"uri":"spotify:track:tr2","name":"Two","trackNumber":2}}
	]}}}}`
	const page2 = `{"data":{"albumUnion":{"tracksV2":{"totalCount":3,"items":[
		{"track":{"uri":"spotify:track:tr3","name":"Three","trackNumber":3}}
	]}}}}`
	var offsets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		offset := offsetOf(t, raw)
		offsets = append(offsets, offset)
		if offset == 0 {
			_, _ = w.Write([]byte(page1))
			return
		}
		_, _ = w.Write([]byte(page2))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderSpotify, "album-1")
	if err != nil {
		t.Fatalf("GetAlbumTracks error = %v", err)
	}
	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != 50 {
		t.Errorf("offsets = %v, want [0 50]", offsets)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks = %d, want 3 across pages", len(tracks))
	}
	for i, want := range []string{"One", "Two", "Three"} {
		if tracks[i].Title != want {
			t.Errorf("track[%d] = %q, want %q (album order preserved across pages)", i, tracks[i].Title, want)
		}
	}
}
