package applemusic_test

import (
	"altune/go-api/internal/search/internal/adapters/applemusic"
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	appleMusicSitePath   = "/us/search"
	appleMusicBundlePath = "/assets/index~abc123.js"
	appleMusicOrigin     = "https://music.apple.com"
	appleMusicUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

type appleMusicSite struct {
	adapter *applemusic.AppleMusicAdapter
	scrapes *atomic.Int32
}

func newAppleMusicSite(t *testing.T, api http.HandlerFunc, tokens ...string) appleMusicSite {
	t.Helper()
	if len(tokens) == 0 {
		tokens = []string{appleMusicFixtureJWT}
	}
	scrapes := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case appleMusicSitePath:
			scrapes.Add(1)
			_, _ = w.Write([]byte(`<html><script src="assets/index~abc123.js"></script></html>`))
		case appleMusicBundlePath:
			_, _ = w.Write([]byte(`var t = "` + tokens[min(int(scrapes.Load()), len(tokens))-1] + `";`))
		default:
			api(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return appleMusicSite{adapter: applemusic.NewAppleMusicAdapter(contracttest.NewClient(srv.URL), clock.Now), scrapes: scrapes}
}

func newTestAppleMusicAdapter(t *testing.T, api http.HandlerFunc) *applemusic.AppleMusicAdapter {
	t.Helper()
	return newAppleMusicSite(t, api).adapter
}

func appleMusicJWT(exp int64, sig string) string {
	return appleMusicJWTFrom("AMPWebPlay", exp, sig)
}

func appleMusicJWTFrom(iss string, exp int64, sig string) string {
	claims := fmt.Sprintf(`{"iss":%q,"exp":%d}`, iss, exp)
	return "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + "." + sig
}

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
	api := func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+appleMusicFixtureJWT {
			t.Errorf("Authorization header = %q, want Bearer <fixture JWT>", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amFixtureResponse))
	}

	a := newTestAppleMusicAdapter(t, api)
	results, err := a.Search(t.Context(), "Blinding Lights", contracttest.AllKinds())
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
	if track.Explicit == nil || !*track.Explicit {
		t.Errorf("track Explicit = %v, want true", track.Explicit)
	}
	if len(track.Sources) != 1 || track.Sources[0].ExternalID != "1488408568" {
		t.Errorf("track source = %+v", track.Sources)
	}

	album := byKind[domain.ResultKindAlbum]
	if album.TrackCount != 14 {
		t.Errorf("album.TrackCount = %d, want 14", album.TrackCount)
	}
	if _, ok := album.Extras["upc"]; ok {
		t.Errorf("album upc extra = %v, want the UPC only in the typed field", album.Extras["upc"])
	}
	if album.UPC != "00602435610238" {
		t.Errorf("album.UPC = %q, want typed upc (merge's album tier reads it)", album.UPC)
	}
	if album.Explicit == nil || !*album.Explicit {
		t.Errorf("album Explicit = %v, want true", album.Explicit)
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
	api := func(w http.ResponseWriter, r *http.Request) {
		gotTypes = r.URL.Query().Get("types")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{}}`))
	}

	a := newTestAppleMusicAdapter(t, api)
	_, err := a.Search(t.Context(), "x", map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if gotTypes != "artists" {
		t.Errorf("types param = %q, want artists", gotTypes)
	}
}

const appleMusicFixtureJWT = "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9." +
	"eyJpc3MiOiJBTVBXZWJQbGF5IiwiZXhwIjo5OTk5OTk5OTk5fQ" +
	".sig"

func TestAppleMusicAdapter_Search_reResolvesTokenOnAuthFailure(t *testing.T) {
	refreshed := appleMusicJWT(9999999999, "refreshed")
	calls := 0
	api := func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+refreshed {
			t.Errorf("Authorization header on retry = %q, want the freshly re-resolved token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amFixtureResponse))
	}
	site := newAppleMusicSite(t, api, appleMusicFixtureJWT, refreshed)

	results, err := site.adapter.Search(t.Context(), "Blinding Lights", contracttest.AllKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial 401 then retry after re-resolve)", calls)
	}
	if site.scrapes.Load() != 2 {
		t.Errorf("page scraped %d times, want 2 (the 401 drops the cached token)", site.scrapes.Load())
	}
	if len(results) != 3 {
		t.Errorf("results = %d, want 3", len(results))
	}
}

func searchTwice(t *testing.T, a *applemusic.AppleMusicAdapter) {
	t.Helper()
	for i := range 2 {
		if _, err := a.Search(t.Context(), "x", contracttest.AllKinds()); err != nil {
			t.Fatalf("Search #%d: %v", i+1, err)
		}
	}
}

func emptyCatalog(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"results":{}}`))
}

func TestAppleMusicAdapter_unexpiredTokenIsScrapedOnceAndReused(t *testing.T) {
	site := newAppleMusicSite(t, emptyCatalog)
	searchTwice(t, site.adapter)
	if site.scrapes.Load() != 1 {
		t.Errorf("page scraped %d times, want 1 (the token's exp is far in the future)", site.scrapes.Load())
	}
}

func TestAppleMusicAdapter_expiredTokenIsReScraped(t *testing.T) {
	expired := appleMusicJWT(time.Now().Add(-time.Minute).Unix(), "expired")
	site := newAppleMusicSite(t, emptyCatalog, expired)
	searchTwice(t, site.adapter)
	if site.scrapes.Load() != 2 {
		t.Errorf("page scraped %d times, want 2 (an expired token is never reused)", site.scrapes.Load())
	}
}

func TestAppleMusicAdapter_skipsABundleJWTFromAnotherIssuer(t *testing.T) {
	otherIssuer := appleMusicJWTFrom("SomeOtherIssuer", 9999999999, "other")
	var got string
	api := func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"results":{}}`))
	}
	site := newAppleMusicSite(t, api, otherIssuer+`"; var dev = "`+appleMusicFixtureJWT)

	if _, err := site.adapter.Search(t.Context(), "x", contracttest.AllKinds()); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "Bearer "+appleMusicFixtureJWT {
		t.Errorf("Authorization = %q, want the AMPWebPlay token that follows the other issuer's", got)
	}
}

func TestAppleMusicAdapter_bundleWithoutATokenFailsBeforeTheAPI(t *testing.T) {
	api := func(w http.ResponseWriter, _ *http.Request) {
		t.Error("catalog API called without a token")
		w.WriteHeader(http.StatusInternalServerError)
	}
	site := newAppleMusicSite(t, api, "no tokens here")
	_, err := site.adapter.Search(t.Context(), "x", contracttest.AllKinds())
	if err == nil || !strings.Contains(err.Error(), "no anonymous devToken") {
		t.Fatalf("err = %v, want the missing-devToken error", err)
	}
}

func TestAppleMusicAdapter_Name(t *testing.T) {
	a := applemusic.NewAppleMusicAdapter(http.DefaultClient, clock.Now)
	if got := a.Name(); got != domain.ProviderAppleMusic {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderAppleMusic)
	}
}

func TestAppleMusicAdapter_GetAlbumTracks(t *testing.T) {
	const tracksJSON = `{"data":[
		{"id":"6763149554","attributes":{"name":"Tell U Sum","artistName":"Che","trackNumber":1,"durationInMillis":159878,"isrc":"QZJ842604812","artwork":{"url":"https://example.com/{w}x{h}bb.jpg"}}},
		{"id":"6763149555","attributes":{"name":"Second","artistName":"Che","trackNumber":2,"durationInMillis":120000}}
	]}`
	api := func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/albums/al-1/tracks") {
			t.Errorf("path = %q, want the album tracks relationship", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+appleMusicFixtureJWT {
			t.Errorf("Authorization = %q, want Bearer <fixture JWT>", got)
		}
		_, _ = w.Write([]byte(tracksJSON))
	}

	a := newTestAppleMusicAdapter(t, api)
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

func appleMusicCallSites() []struct {
	name       string
	call       func(ctx context.Context, a *applemusic.AppleMusicAdapter) error
	decodeText string
} {
	all := map[domain.ResultKind]bool{domain.ResultKindTrack: true, domain.ResultKindAlbum: true, domain.ResultKindArtist: true}
	return []struct {
		name       string
		call       func(ctx context.Context, a *applemusic.AppleMusicAdapter) error
		decodeText string
	}{
		{"search", func(ctx context.Context, a *applemusic.AppleMusicAdapter) error {
			_, err := a.Search(ctx, "q", all)
			return err
		}, "decode catalog search response: "},
		{"album tracks", func(ctx context.Context, a *applemusic.AppleMusicAdapter) error {
			_, err := a.GetAlbumTracks(ctx, domain.ProviderAppleMusic, "al-1")
			return err
		}, "decode album tracks response: "},
		{"artist albums", func(ctx context.Context, a *applemusic.AppleMusicAdapter) error {
			_, err := a.GetArtistAlbums(ctx, domain.ProviderAppleMusic, "ar-1")
			return err
		}, "decode catalog response: "},
		{"artist top tracks", func(ctx context.Context, a *applemusic.AppleMusicAdapter) error {
			_, err := a.GetArtistTopTracks(ctx, domain.ProviderAppleMusic, "ar-1")
			return err
		}, "decode catalog response: "},
	}
}

func TestAppleMusicAdapter_callSites_sendRequestHeaders(t *testing.T) {
	for _, tc := range appleMusicCallSites() {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			api := func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				_, _ = w.Write([]byte(`{}`))
			}

			if err := tc.call(t.Context(), newTestAppleMusicAdapter(t, api)); err != nil {
				t.Fatalf("call: %v", err)
			}
			want := map[string]string{
				"Authorization": "Bearer " + appleMusicFixtureJWT,
				"Origin":        appleMusicOrigin,
				"User-Agent":    appleMusicUserAgent,
			}
			for k, v := range want {
				if got.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, got.Get(k), v)
				}
			}
		})
	}
}

func TestAppleMusicAdapter_callSites_non200IsStatusError(t *testing.T) {
	for _, tc := range appleMusicCallSites() {
		t.Run(tc.name, func(t *testing.T) {
			api := func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}

			err := tc.call(t.Context(), newTestAppleMusicAdapter(t, api))
			if err == nil || err.Error() != "http status 500" {
				t.Fatalf("err = %v, want exactly %q", err, "http status 500")
			}
		})
	}
}

func TestAppleMusicAdapter_callSites_malformedBodyIsWrappedDecodeError(t *testing.T) {
	for _, tc := range appleMusicCallSites() {
		t.Run(tc.name, func(t *testing.T) {
			api := func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`<html>maintenance</html>`))
			}

			err := tc.call(t.Context(), newTestAppleMusicAdapter(t, api))
			if err == nil || !strings.HasPrefix(err.Error(), tc.decodeText) {
				t.Fatalf("err = %v, want prefix %q", err, tc.decodeText)
			}
		})
	}
}

func TestAlbumTracksLive_E2E(t *testing.T) {
	if os.Getenv("SPOTIFY_LIVE") != "1" {
		t.Skip("set SPOTIFY_LIVE=1")
	}
	ctx := context.Background()
	client := &http.Client{Timeout: 15 * time.Second}

	ap := applemusic.NewAppleMusicAdapter(client, clock.Now)
	apTracks, err := ap.GetAlbumTracks(ctx, domain.ProviderAppleMusic, "1895185526")
	if err != nil {
		t.Fatalf("apple GetAlbumTracks: %v", err)
	}
	if len(apTracks) == 0 {
		t.Fatal("apple returned 0 tracks")
	}
	t.Logf("apple Para'dies: %d tracks; first %q by %q", len(apTracks), apTracks[0].Title, apTracks[0].Subtitle)
}

func TestNewAppleMusicAdapter_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewAppleMusicAdapter without a clock did not panic")
		}
	}()

	applemusic.NewAppleMusicAdapter(http.DefaultClient, nil)
}
