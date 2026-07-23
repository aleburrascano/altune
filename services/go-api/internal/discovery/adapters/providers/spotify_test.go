package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

var farFuture = time.Now().Add(24 * time.Hour)

// --- TOTP -----------------------------------------------------------------

func TestSpotifyTOTPGenerate_isDeterministicSixDigits(t *testing.T) {
	code1 := spotifyTOTPGenerate(spotifyTOTPSecrets[0].secret, 1700000000)
	code2 := spotifyTOTPGenerate(spotifyTOTPSecrets[0].secret, 1700000000)
	if code1 != code2 {
		t.Errorf("same input produced different codes: %q vs %q", code1, code2)
	}
	if len(code1) != 6 {
		t.Errorf("code length = %d, want 6 (zero-padded): %q", len(code1), code1)
	}

	// A different 30s time-step must (almost certainly) produce a different
	// code — cheap sanity check that time actually feeds the computation.
	code3 := spotifyTOTPGenerate(spotifyTOTPSecrets[0].secret, 1700000000+30)
	if code1 == code3 {
		t.Errorf("codes for different time steps collided: both %q (extremely unlikely, check the counter math)", code1)
	}
}

func TestSpotifyTOTPKey_matchesKnownXORDerivation(t *testing.T) {
	// "A" (0x41) at index 0: 0x41 ^ (0%33+9) = 65 ^ 9  = 72 -> "72"
	// "B" (0x42) at index 1: 0x42 ^ (1%33+9) = 66 ^ 10 = 72 -> "72"
	got := string(spotifyTOTPKey("AB"))
	if got != "7272" {
		t.Errorf("spotifyTOTPKey(%q) = %q, want %q", "AB", got, "7272")
	}
}

// --- adapter / mapping ------------------------------------------------

func newTestSpotifyAdapter(searchSrv *httptest.Server) *SpotifyAdapter {
	a := NewSpotifyAdapter(searchSrv.Client())
	a.pathfinderURL = searchSrv.URL
	a.resolver.cached = &spotifySession{
		accessToken:  "test-access-token",
		accessExpiry: farFuture,
		clientToken:  "test-client-token",
		clientExpiry: farFuture,
	}
	return a
}

// spotifyFixtureResponse is a trimmed searchDesktop response covering one
// track, one album, and one artist. Shape (including tracksV2's extra
// items[].item.data hop vs albumsV2/artists' items[].data) captured live
// against api-partner.spotify.com (2026-07-22).
const spotifyFixtureResponse = `{
  "data": {
    "searchV2": {
      "tracksV2": {
        "items": [{
          "item": {
            "data": {
              "id": "0VjIjW4GlUZAMYd2vXMi3b",
              "name": "Blinding Lights",
              "uri": "spotify:track:0VjIjW4GlUZAMYd2vXMi3b",
              "duration": {"totalMilliseconds": 200000},
              "contentRating": {"label": "NONE"},
              "albumOfTrack": {
                "id": "4yP0hdKOZPNshxUOjY0cZj",
                "name": "After Hours",
                "uri": "spotify:album:4yP0hdKOZPNshxUOjY0cZj",
                "coverArt": {"sources": [
                  {"url": "https://example.com/64.jpg", "width": 64, "height": 64},
                  {"url": "https://example.com/640.jpg", "width": 640, "height": 640}
                ]}
              },
              "artists": {"items": [{"profile": {"name": "The Weeknd"}, "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ"}]}
            }
          }
        }]
      },
      "albumsV2": {
        "items": [{
          "data": {
            "id": "4yP0hdKOZPNshxUOjY0cZj",
            "name": "After Hours",
            "uri": "spotify:album:4yP0hdKOZPNshxUOjY0cZj",
            "coverArt": {"sources": [{"url": "https://example.com/album.jpg", "width": 300, "height": 300}]}
          }
        }]
      },
      "artists": {
        "items": [{
          "data": {
            "id": "1Xyo4u8uXC1ZmMpatF05PJ",
            "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ",
            "profile": {"name": "The Weeknd"},
            "visuals": {"avatarImage": {"sources": [{"url": "https://example.com/artist.jpg", "width": 500, "height": 500}]}}
          }
        }]
      }
    }
  }
}`

func TestSpotifyAdapter_Search_mapsAllKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q, want Bearer test-access-token", got)
		}
		if got := r.Header.Get("client-token"); got != "test-client-token" {
			t.Errorf("client-token = %q, want test-client-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spotifyFixtureResponse))
	}))
	defer srv.Close()

	a := newTestSpotifyAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3 (track+album+artist)", len(results))
	}

	byKind := map[domain.ResultKind]domain.SearchResult{}
	for _, r := range results {
		byKind[r.Kind] = r
	}

	track := byKind[domain.ResultKindTrack]
	if track.Title != "Blinding Lights" || track.Subtitle != "The Weeknd" {
		t.Errorf("track = %+v", track)
	}
	if track.Album != "After Hours" {
		t.Errorf("track.Album = %q, want After Hours", track.Album)
	}
	if track.Duration != 200 {
		t.Errorf("track.Duration = %d, want 200", track.Duration)
	}
	if track.ImageURL != "https://example.com/640.jpg" {
		t.Errorf("track.ImageURL = %q, want the widest source (640.jpg)", track.ImageURL)
	}
	if len(track.Sources) != 1 || track.Sources[0].ExternalID != "0VjIjW4GlUZAMYd2vXMi3b" {
		t.Errorf("track source = %+v", track.Sources)
	}

	album := byKind[domain.ResultKindAlbum]
	if len(album.Sources) != 1 || album.Sources[0].ExternalID != "4yP0hdKOZPNshxUOjY0cZj" {
		t.Errorf("album source = %+v", album.Sources)
	}

	artist := byKind[domain.ResultKindArtist]
	if artist.Title != "The Weeknd" {
		t.Errorf("artist.Title = %q", artist.Title)
	}
}

func TestSpotifyAdapter_Search_graphqlErrorIsSurfaced(t *testing.T) {
	// A stale persisted-query hash: pathfinder answers HTTP 200 with a GraphQL
	// "errors" array and no data. This must surface as an error, not decode to a
	// silent empty result set (the failure mode that made Spotify vanish from
	// search while still looking healthy on the provider board).
	const persistedQueryNotFound = `{"errors":[{"message":"PersistedQueryNotFound"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(persistedQueryNotFound))
	}))
	defer srv.Close()

	a := newTestSpotifyAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err == nil {
		t.Fatalf("Search() error = nil, want a surfaced GraphQL error (got %d results)", len(results))
	}
	if results != nil {
		t.Errorf("results = %+v, want nil on a GraphQL error", results)
	}
}

func TestSpotifyAdapter_Search_explicitFlag(t *testing.T) {
	explicitResponse := `{"data":{"searchV2":{"tracksV2":{"items":[{"item":{"data":{
		"id":"x","name":"Explicit Track","uri":"spotify:track:x",
		"contentRating":{"label":"EXPLICIT"},
		"albumOfTrack":{"id":"a","name":"Album","uri":"spotify:album:a","coverArt":{"sources":[]}},
		"artists":{"items":[{"profile":{"name":"Artist"},"uri":"spotify:artist:a"}]}
	}}}]},"albumsV2":{"items":[]},"artists":{"items":[]}}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(explicitResponse))
	}))
	defer srv.Close()

	a := newTestSpotifyAdapter(srv)
	results, err := a.Search(t.Context(), "x", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Extras["explicit"] != true {
		t.Errorf("results = %+v, want one track with explicit=true", results)
	}
}

func TestSpotifyAdapter_Search_reResolvesSessionOnAuthFailure(t *testing.T) {
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
		_, _ = w.Write([]byte(spotifyFixtureResponse))
	}))
	defer srv.Close()

	// A fake token backend plays server-time, access-token, and client-token
	// so invalidate() -> re-resolve has somewhere real to land.
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
	a.resolver.cached = &spotifySession{
		accessToken: "stale-token", accessExpiry: farFuture,
		clientToken: "stale-client-token", clientExpiry: farFuture,
	}
	a.resolver.serverTimeURL = tokenSrv.URL + "/server-time"
	a.resolver.accessTokenURL = tokenSrv.URL + "/token"
	a.resolver.clientTokenURL = tokenSrv.URL + "/clienttoken"

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

func TestSpotifyAdapter_Search_filtersByRequestedKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spotifyFixtureResponse))
	}))
	defer srv.Close()

	a := newTestSpotifyAdapter(srv)
	results, err := a.Search(t.Context(), "x", map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Kind != domain.ResultKindArtist {
		t.Errorf("results = %+v, want exactly one artist", results)
	}
}

func TestSpotifyAdapter_Name(t *testing.T) {
	a := NewSpotifyAdapter(http.DefaultClient)
	if got := a.Name(); got != domain.ProviderSpotify {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderSpotify)
	}
}

// --- token resolver ---------------------------------------------------

func TestSpotifyTokenResolver_resolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/server-time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": 1700000000})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":                      "resolved-token",
				"accessTokenExpirationTimestampMs": 99999999999999,
			})
		case "/clienttoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"granted_token": map[string]any{"token": "resolved-client-token", "expires_after_seconds": 999999},
			})
		}
	}))
	defer srv.Close()

	r := newSpotifyTokenResolver(srv.Client())
	r.serverTimeURL = srv.URL + "/server-time"
	r.accessTokenURL = srv.URL + "/token"
	r.clientTokenURL = srv.URL + "/clienttoken"

	sess, err := r.get(t.Context())
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if sess.accessToken != "resolved-token" || sess.clientToken != "resolved-client-token" {
		t.Errorf("session = %+v", sess)
	}
}

func TestSpotifyTokenResolver_fallsBackOnTotpVerExpired(t *testing.T) {
	var seenVersions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/server-time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": 1700000000})
		case "/token":
			ver := r.URL.Query().Get("totpVer")
			seenVersions = append(seenVersions, ver)
			if ver == "61" {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "totpVerExpired"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":                      "token-for-" + ver,
				"accessTokenExpirationTimestampMs": 99999999999999,
			})
		case "/clienttoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"granted_token": map[string]any{"token": "ct", "expires_after_seconds": 999999},
			})
		}
	}))
	defer srv.Close()

	r := newSpotifyTokenResolver(srv.Client())
	r.serverTimeURL = srv.URL + "/server-time"
	r.accessTokenURL = srv.URL + "/token"
	r.clientTokenURL = srv.URL + "/clienttoken"

	sess, err := r.get(t.Context())
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if sess.accessToken != "token-for-60" {
		t.Errorf("accessToken = %q, want token-for-60 (fell back past the expired v61)", sess.accessToken)
	}
	if len(seenVersions) < 2 || seenVersions[0] != "61" {
		t.Errorf("seenVersions = %v, want to start with 61 and fall back", seenVersions)
	}
}
