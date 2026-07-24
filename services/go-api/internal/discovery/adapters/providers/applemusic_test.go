package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// newTestAppleMusicAdapter seeds a fake token so tests exercise the catalog
// search call without needing a live page-scrape round trip — the same shape
// as newTestSoundCloudAPI seeding a client_id.
func newTestAppleMusicAdapter(srv *httptest.Server) *AppleMusicAdapter {
	a := NewAppleMusicAdapter(srv.Client())
	a.searchURL = srv.URL
	a.resolver.cached = "test-token"
	a.resolver.expiry = time.Now().Add(time.Hour)
	return a
}

// amFixtureResponse is a trimmed catalog search response covering one song,
// one album, and one artist. Shape captured live against api.music.apple.com
// (2026-07-22).
const amFixtureResponse = `{
  "results": {
    "songs": {"data": [{
      "id": "1488408568",
      "attributes": {
        "name": "Blinding Lights",
        "artistName": "The Weeknd",
        "albumName": "After Hours",
        "artwork": {"url": "https://example.com/{w}x{h}bb.jpg"},
        "composerName": "Max Martin, Oscar Holter, Abel Tesfaye",
        "genreNames": ["R&B/Soul", "Music"],
        "durationInMillis": 201570,
        "discNumber": 1,
        "trackNumber": 1,
        "hasLyrics": true,
        "isAppleDigitalMaster": true,
        "isrc": "USUG11904206",
        "contentRating": "explicit",
        "previews": [{"url": "https://audio-ssl.itunes.apple.com/preview.m4a"}],
        "releaseDate": "2019-11-29",
        "url": "https://music.apple.com/us/song/1488408568"
      }
    }]},
    "albums": {"data": [{
      "id": "1488408555",
      "attributes": {
        "name": "After Hours",
        "artistName": "The Weeknd",
        "artwork": {"url": "https://example.com/album/{w}x{h}bb.jpg"},
        "contentRating": "explicit",
        "copyright": "2020 The Weeknd XO, Inc.",
        "genreNames": ["Pop"],
        "isSingle": false,
        "recordLabel": "Republic Records",
        "releaseDate": "2020-03-20",
        "trackCount": 14,
        "upc": "00602435610238",
        "url": "https://music.apple.com/us/album/1488408555"
      }
    }]},
    "artists": {"data": [{
      "id": "479756766",
      "attributes": {
        "name": "The Weeknd",
        "artwork": {"url": "https://example.com/artist/{w}x{h}bb.jpg"},
        "genreNames": ["R&B/Soul"],
        "editorialNotes": {"short": "A subversive vocalist who emerged from the underground."},
        "url": "https://music.apple.com/us/artist/479756766"
      }
    }]}
  }
}`

func TestAppleMusicAdapter_Search_mapsAllKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want Bearer test-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amFixtureResponse))
	}))
	defer srv.Close()

	a := newTestAppleMusicAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3 (song+album+artist)", len(results))
	}

	byKind := map[domain.ResultKind]domain.SearchResult{}
	for _, r := range results {
		byKind[r.Kind] = r
	}

	track := byKind[domain.ResultKindTrack]
	if track.Title != "Blinding Lights" || track.Subtitle != "The Weeknd" {
		t.Errorf("track = %+v", track)
	}
	if track.ISRC != "USUG11904206" {
		t.Errorf("track.ISRC = %q, want USUG11904206", track.ISRC)
	}
	if track.Album != "After Hours" {
		t.Errorf("track.Album = %q, want After Hours", track.Album)
	}
	if track.Duration != 201 {
		t.Errorf("track.Duration = %d, want 201", track.Duration)
	}
	if track.Extras["composer"] != "Max Martin, Oscar Holter, Abel Tesfaye" {
		t.Errorf("track composer extra = %v", track.Extras["composer"])
	}
	if track.ImageURL != "https://example.com/1000x1000bb.jpg" {
		t.Errorf("track.ImageURL = %q, want the {w}x{h} template filled with 1000x1000", track.ImageURL)
	}
	if track.Extras["preview_url"] != "https://audio-ssl.itunes.apple.com/preview.m4a" {
		t.Errorf("track preview_url extra = %v", track.Extras["preview_url"])
	}
	if track.Extras["explicit"] != true {
		t.Errorf("track explicit extra = %v, want true", track.Extras["explicit"])
	}
	if len(track.Sources) != 1 || track.Sources[0].ExternalID != "1488408568" {
		t.Errorf("track source = %+v", track.Sources)
	}

	album := byKind[domain.ResultKindAlbum]
	if album.TrackCount != 14 {
		t.Errorf("album.TrackCount = %d, want 14", album.TrackCount)
	}
	if album.Extras["upc"] != "00602435610238" {
		t.Errorf("album upc extra = %v", album.Extras["upc"])
	}
	if album.UPC != "00602435610238" {
		t.Errorf("album.UPC = %q, want typed upc (merge's album tier reads it)", album.UPC)
	}
	if album.Extras["explicit"] != true {
		t.Errorf("album explicit extra = %v, want true", album.Extras["explicit"])
	}

	artist := byKind[domain.ResultKindArtist]
	if artist.Title != "The Weeknd" {
		t.Errorf("artist.Title = %q", artist.Title)
	}
	if artist.Extras["bio"] == "" {
		t.Errorf("artist bio extra missing")
	}
}

func TestAppleMusicAdapter_Search_onlyRequestsAskedKinds(t *testing.T) {
	var gotTypes string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTypes = r.URL.Query().Get("types")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{}}`))
	}))
	defer srv.Close()

	a := newTestAppleMusicAdapter(srv)
	_, err := a.Search(t.Context(), "x", map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if gotTypes != "artists" {
		t.Errorf("types param = %q, want artists", gotTypes)
	}
}

// appleMusicFixtureJWT is a syntactically valid (unsigned-verification-wise;
// nothing here checks the signature) JWT whose payload identifies it as the
// anonymous web-player token, for resolver tests.
const appleMusicFixtureJWT = "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9." +
	"eyJpc3MiOiJBTVBXZWJQbGF5IiwiZXhwIjo5OTk5OTk5OTk5fQ" + // {"iss":"AMPWebPlay","exp":9999999999}
	".sig"

func TestAppleMusicAdapter_Search_reResolvesTokenOnAuthFailure(t *testing.T) {
	calls := 0
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+appleMusicFixtureJWT {
			t.Errorf("Authorization header on retry = %q, want the freshly re-resolved token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amFixtureResponse))
	}))
	defer searchSrv.Close()

	// A separate fake server plays both the page (script tag) and the bundle
	// (embedded JWT) so invalidate() -> re-resolve has somewhere real to land.
	bundleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "index~abc123.js") {
			_, _ = w.Write([]byte(`var t = "` + appleMusicFixtureJWT + `";`))
			return
		}
		_, _ = w.Write([]byte(`<script src="assets/index~abc123.js"></script>`))
	}))
	defer bundleSrv.Close()

	a := newTestAppleMusicAdapter(searchSrv)
	a.resolver.cached = "stale-token" // seeded valid so the first Search call skips resolve()
	a.resolver.expiry = time.Now().Add(time.Hour)
	a.resolver.siteURL = bundleSrv.URL
	a.resolver.bundleBaseURL = bundleSrv.URL + "/"

	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial 401 then retry after re-resolve)", calls)
	}
	if len(results) != 3 {
		t.Errorf("results = %d, want 3", len(results))
	}
}

func TestAppleMusicTokenResolver_resolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "index~abc123.js") {
			_, _ = w.Write([]byte(`var t = "` + appleMusicFixtureJWT + `";`))
			return
		}
		_, _ = w.Write([]byte(`<html><script src="assets/index~abc123.js"></script></html>`))
	}))
	defer srv.Close()

	r := newAppleMusicTokenResolver(srv.Client())
	r.siteURL = srv.URL
	r.bundleBaseURL = srv.URL + "/"

	token, err := r.get(t.Context())
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if token != appleMusicFixtureJWT {
		t.Errorf("token = %q, want the fixture JWT", token)
	}
}

func TestExtractAppleMusicToken(t *testing.T) {
	token, expiry, ok := extractAppleMusicToken(`var t = "` + appleMusicFixtureJWT + `";`)
	if !ok {
		t.Fatalf("extractAppleMusicToken did not find the token")
	}
	if token != appleMusicFixtureJWT {
		t.Errorf("token = %q, want fixture JWT", token)
	}
	if expiry.Unix() != 9999999999 {
		t.Errorf("expiry = %v, want unix 9999999999", expiry)
	}

	if _, _, ok := extractAppleMusicToken(`no tokens here`); ok {
		t.Errorf("expected no token found in a bundle with none")
	}
}

func TestAppleMusicAdapter_Name(t *testing.T) {
	a := NewAppleMusicAdapter(http.DefaultClient)
	if got := a.Name(); got != domain.ProviderAppleMusic {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderAppleMusic)
	}
}

func TestAppleMusicAdapter_GetAlbumTracks(t *testing.T) {
	const tracksJSON = `{"data":[
		{"id":"6763149554","attributes":{"name":"Tell U Sum","artistName":"Che","trackNumber":1,"durationInMillis":159878,"isrc":"QZJ842604812","artwork":{"url":"https://example.com/{w}x{h}bb.jpg"}}},
		{"id":"6763149555","attributes":{"name":"Second","artistName":"Che","trackNumber":2,"durationInMillis":120000}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/albums/al-1/tracks") {
			t.Errorf("path = %q, want the album tracks relationship", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		_, _ = w.Write([]byte(tracksJSON))
	}))
	defer srv.Close()

	a := newTestAppleMusicAdapter(srv)
	a.catalogBase = srv.URL // GetAlbumTracks builds off catalogBase, not searchURL
	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderAppleMusic, "al-1")
	if err != nil {
		t.Fatalf("GetAlbumTracks error = %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks = %d, want 2 (album order preserved)", len(tracks))
	}
	if tracks[0].Title != "Tell U Sum" || tracks[0].Subtitle != "Che" || tracks[0].Duration != 159 {
		t.Errorf("track[0] = %+v", tracks[0])
	}
	if tracks[0].ISRC != "QZJ842604812" {
		t.Errorf("track[0].ISRC = %q, want QZJ842604812", tracks[0].ISRC)
	}
}
