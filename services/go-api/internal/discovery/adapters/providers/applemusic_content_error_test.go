package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

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

// The content path shares Search's rotation tolerance: a 401 invalidates the
// token, re-resolves, and retries once.
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
