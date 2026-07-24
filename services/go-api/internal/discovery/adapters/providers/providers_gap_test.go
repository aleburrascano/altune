package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// Remaining risk-path coverage: iTunes top-tracks delegation, MB release-group
// titles + track-kind ResolveMBID, the YT Music artwork resolver, the Spotify
// content-side auth retry, and SoundCloud's re-resolve failure surface.

func TestITunesAdapter_GetArtistTopTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("entity"); got != "song" {
			t.Errorf("entity = %q, want song", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resultCount": 2, "results": [
			{"wrapperType": "artist", "artistName": "Che"},
			{"wrapperType": "track", "trackId": 111, "trackName": "Los Santos", "artistName": "Che", "trackTimeMillis": 125000}
		]}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	tracks, err := adapter.GetArtistTopTracks(context.Background(), domain.ProviderITunes, "42")
	if err != nil {
		t.Fatalf("GetArtistTopTracks: %v", err)
	}
	// The parent "artist" wrapper must be filtered; only track children map.
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1 (parent wrapper dropped)", len(tracks))
	}
	if tracks[0].Kind != domain.ResultKindTrack || tracks[0].Title != "Los Santos" {
		t.Errorf("track = %+v", tracks[0])
	}
}

func TestMusicBrainzAdapter_ReleaseGroupTitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"release-group-count": 2,
			"release-groups": [
				` + mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `,
				` + mbReleaseGroupJSON("rg-2", "Kid A", "Radiohead", "mbid-rh") + `
			]}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
	if err != nil {
		t.Fatalf("ReleaseGroupTitles: %v", err)
	}
	if len(titles) != 2 || titles[0] != "OK Computer" || titles[1] != "Kid A" {
		t.Errorf("titles = %v, want the release-group titles in order", titles)
	}
}

func TestMusicBrainzAdapter_ResolveMBID_track(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recordings": [
			{"id": "rec-cover", "title": "Paranoid Android", "artist-credit": [{"name": "Cover Band"}]},
			{"id": "rec-real", "title": "Paranoid Android", "artist-credit": [{"name": "Radiohead"}]}
		]}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	mbid, err := adapter.ResolveMBID(context.Background(), domain.ResultKindTrack, "Paranoid Android", "Radiohead")
	if err != nil {
		t.Fatalf("ResolveMBID: %v", err)
	}
	if mbid != "rec-real" {
		t.Errorf("mbid = %q, want the credit-matched recording (strict, no fuzzy guess)", mbid)
	}
}

func TestYouTubeMusicArtworkResolver_Resolve(t *testing.T) {
	t.Run("artist image resized to hero", func(t *testing.T) {
		srv := serveYTMFixture(t, "ytmusic_artist_filter_sombr.json")
		defer srv.Close()

		r := NewYouTubeMusicArtworkResolver(&redirectTransport{targetURL: srv.URL})
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "sombr", "", "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if url == "" {
			t.Fatal("expected an artist artwork URL from the fixture")
		}
		if !strings.Contains(url, "w1000-h1000") {
			t.Errorf("url = %q, want the w1000-h1000 hero resize", url)
		}
	})

	t.Run("search error degrades to empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<html>denied</html>`))
		}))
		defer srv.Close()

		r := NewYouTubeMusicArtworkResolver(&redirectTransport{targetURL: srv.URL})
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "sombr", "", "")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil) — the chain degrades", url, err)
		}
	})

	t.Run("empty title is a no-op", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected for an empty title")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		r := NewYouTubeMusicArtworkResolver(&redirectTransport{targetURL: srv.URL})
		url, err := r.Resolve(context.Background(), domain.ResultKindArtist, "", "", "")
		if err != nil || url != "" {
			t.Errorf("Resolve = (%q, %v), want (\"\", nil)", url, err)
		}
	})
}

// The content side (pathfinderContent) shares Search's rotation tolerance: a
// 401 invalidates the session, re-resolves, and retries once.
func TestSpotifyAdapter_GetArtistAlbums_reResolvesSessionOnAuthFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh-access-token" {
			t.Errorf("Authorization on retry = %q, want the freshly re-resolved token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"artistUnion": {"discography": {"all": {"items": [
			{"releases": {"items": [{
				"id": "alb-1", "name": "After Hours", "type": "ALBUM",
				"date": {"isoString": "2020-03-20T00:00:00Z", "year": 2020},
				"tracks": {"totalCount": 14}
			}]}}
		]}}}}}`))
	}))
	defer srv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/server-time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": 1700000000})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":                      "fresh-access-token",
				"accessTokenExpirationTimestampMs": 99999999999999,
			})
		case "/clienttoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"granted_token": map[string]any{"token": "fresh-client-token", "expires_after_seconds": 999999},
			})
		}
	}))
	defer tokenSrv.Close()

	a := newTestSpotifyAdapter(srv)
	a.resolver.serverTimeURL = tokenSrv.URL + "/server-time"
	a.resolver.accessTokenURL = tokenSrv.URL + "/token"
	a.resolver.clientTokenURL = tokenSrv.URL + "/clienttoken"

	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial 401 then retry after re-resolve)", calls)
	}
	if len(albums) != 1 || albums[0].Title != "After Hours" {
		t.Errorf("albums = %+v", albums)
	}
}

// When the auth retry's client_id re-resolve itself fails, the caller must see
// the re-resolve error, not a silent empty.
func TestSoundCloudAPIAdapter_authRetryReResolveFailureSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/users") {
			w.WriteHeader(http.StatusUnauthorized) // auth failure → invalidate + re-resolve
			return
		}
		// The homepage scrape for the re-resolve fails too.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	a.resolver.siteURL = srv.URL

	_, err := a.searchArtists(context.Background(), "che")
	if err == nil || !strings.Contains(err.Error(), "re-resolve client_id") {
		t.Fatalf("err = %v, want the re-resolve failure surfaced", err)
	}
}
