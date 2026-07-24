package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// A transient playlist-fetch failure (5xx/timeout) must propagate — falling
// through to the overlapping track-id namespace would resolve an unrelated
// single-track "tracklist". Only a definitive 404 may fall through (covered by
// TestSoundCloud_GetAlbumTracks_playlistAndSingle).
func TestSoundCloud_GetAlbumTracks_transientPlaylistErrorPropagates(t *testing.T) {
	var trackFetches atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/playlists/500"):
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/tracks/500"):
			trackFetches.Add(1)
			_, _ = w.Write([]byte(`{"id":500,"kind":"track","title":"Unrelated Track","user":{"username":"Someone"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	a := newTestSoundCloudAPI(srv, nil)

	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderSoundCloud, "500")
	if err == nil {
		t.Fatalf("expected an error on a 500 playlist fetch, got tracks: %+v", tracks)
	}
	if trackFetches.Load() != 0 {
		t.Errorf("track-id fallback fired on a transient error — would return an unrelated tracklist")
	}
}

// A server that keeps returning empty pages with a next_href must not spin
// until the ctx deadline — the page counter caps the walk.
func TestSoundCloud_doSearch_capsEmptyPageWalk(t *testing.T) {
	var requests atomic.Int64
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"collection":[],"next_href":"` + srvURL + `/search/tracks?offset=next"}`))
	}))
	defer srv.Close()
	srvURL = srv.URL
	a := newTestSoundCloudAPI(srv, nil)

	results, _, err := a.doSearch(context.Background(), "clientid", "query")
	if err != nil {
		t.Fatalf("doSearch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
	if got := requests.Load(); got != scMaxSearchPages {
		t.Errorf("requests = %d, want %d (page cap must stop the empty-page spin)", got, scMaxSearchPages)
	}
}
