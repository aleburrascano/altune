package providers

import (
	"altune/go-api/internal/discovery/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAppleMusicAdapter_GetArtistAlbums(t *testing.T) {
	const albumsJSON = `{"data":[
		{"id":"a1","attributes":{"name":"After Hours","artistName":"The Weeknd","releaseDate":"2020-03-20","trackCount":14,"artwork":{"url":"https://ex/{w}x{h}.jpg"}}},
		{"id":"a2","attributes":{"name":"Starboy","artistName":"The Weeknd","releaseDate":"2016-11-25","trackCount":18,"artwork":{"url":"https://ex/{w}x{h}.jpg"}}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		if !strings.Contains(r.URL.Path, "/artists/artist-1/albums") {
			t.Errorf("path = %q, want the artist albums relationship", r.URL.Path)
		}
		_, _ = w.Write([]byte(albumsJSON))
	}))
	defer srv.Close()

	a := NewAppleMusicAdapter(srv.Client())
	a.catalogBase = srv.URL
	a.resolver.cached = "test-token"
	a.resolver.expiry = farFuture

	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderAppleMusic, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("albums = %d, want 2", len(albums))
	}
	if albums[0].Title != "After Hours" || albums[0].ReleaseDate != "2020-03-20" {
		t.Errorf("album[0] = %+v, want After Hours / 2020-03-20", albums[0])
	}
	if albums[0].ImageURL == "" || strings.Contains(albums[0].ImageURL, "{w}") {
		t.Errorf("album[0].ImageURL = %q, want a filled artwork URL", albums[0].ImageURL)
	}
	if albums[0].TrackCount != 14 {
		t.Errorf("album[0].TrackCount = %d, want 14", albums[0].TrackCount)
	}
	if albums[0].Sources[0].Provider != domain.ProviderAppleMusic {
		t.Errorf("source provider = %v, want applemusic", albums[0].Sources[0].Provider)
	}
}

func TestAppleMusicAdapter_GetArtistTopTracks(t *testing.T) {
	const songsJSON = `{"data":[
		{"id":"s1","attributes":{"name":"Blinding Lights","artistName":"The Weeknd","albumName":"After Hours","isrc":"USUG12000123","releaseDate":"2019-11-29","durationInMillis":200000,"artwork":{"url":"https://ex/{w}x{h}.jpg"}}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/artists/artist-1/view/top-songs") {
			t.Errorf("path = %q, want the top-songs view", r.URL.Path)
		}
		_, _ = w.Write([]byte(songsJSON))
	}))
	defer srv.Close()

	a := NewAppleMusicAdapter(srv.Client())
	a.catalogBase = srv.URL
	a.resolver.cached = "test-token"
	a.resolver.expiry = farFuture

	tracks, err := a.GetArtistTopTracks(t.Context(), domain.ProviderAppleMusic, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistTopTracks error = %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}
	tr := tracks[0]
	if tr.Title != "Blinding Lights" || tr.Subtitle != "The Weeknd" {
		t.Errorf("track = %+v", tr)
	}
	if tr.ISRC != "USUG12000123" || tr.Album != "After Hours" || tr.Duration != 200 {
		t.Errorf("track metadata = %+v", tr)
	}
}

func TestAppleMusicAdapter_GetAlbumTracks_http500IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestAppleMusicAdapter(srv)
	a.catalogBase = srv.URL
	if _, err := a.GetAlbumTracks(t.Context(), domain.ProviderAppleMusic, "al-1"); err == nil {
		t.Fatal("expected an error on a non-auth HTTP 500 (no token re-resolve applies)")
	}
}

func TestAppleMusicAdapter_GetAlbumTracks_malformedJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>maintenance</html>`))
	}))
	defer srv.Close()

	a := newTestAppleMusicAdapter(srv)
	a.catalogBase = srv.URL
	_, err := a.GetAlbumTracks(t.Context(), domain.ProviderAppleMusic, "al-1")
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want a decode error on an HTML-instead-of-JSON body", err)
	}
}

func TestAppleMusicAdapter_fetchCatalog_reResolvesTokenOnAuthFailure(t *testing.T) {
	calls := 0
	catalogSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+appleMusicFixtureJWT {
			t.Errorf("Authorization on retry = %q, want the freshly re-resolved token", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"t1","attributes":{"name":"Song","artistName":"A"}}]}`))
	}))
	defer catalogSrv.Close()

	bundleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "index~abc123.js") {
			_, _ = w.Write([]byte(`var t = "` + appleMusicFixtureJWT + `";`))
			return
		}
		_, _ = w.Write([]byte(`<script src="assets/index~abc123.js"></script>`))
	}))
	defer bundleSrv.Close()

	a := newTestAppleMusicAdapter(catalogSrv)
	a.catalogBase = catalogSrv.URL
	a.resolver.cached = "stale-token"
	a.resolver.expiry = time.Now().Add(time.Hour)
	a.resolver.siteURL = bundleSrv.URL
	a.resolver.bundleBaseURL = bundleSrv.URL + "/"

	tracks, err := a.GetArtistTopTracks(t.Context(), domain.ProviderAppleMusic, "ar-1")
	if err != nil {
		t.Fatalf("GetArtistTopTracks: %v", err)
	}
	if calls != 2 {
		t.Errorf("catalog calls = %d, want 2 (401 then retry)", calls)
	}
	if len(tracks) != 1 || tracks[0].Title != "Song" {
		t.Errorf("tracks = %+v", tracks)
	}
}

func TestAppleMusicAdapter_meta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := newTestAppleMusicAdapter(srv)
	kinds := a.SupportedKinds()
	if !kinds[domain.ResultKindTrack] || !kinds[domain.ResultKindAlbum] || !kinds[domain.ResultKindArtist] {
		t.Errorf("SupportedKinds = %v, want all three", kinds)
	}
	if a.SearchTimeout() != appleMusicSearchTimeout {
		t.Errorf("SearchTimeout = %v, want %v", a.SearchTimeout(), appleMusicSearchTimeout)
	}
	if a.ArtworkSource() == "" {
		t.Error("ArtworkSource must be non-empty")
	}
}
