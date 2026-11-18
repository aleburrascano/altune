package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

type spotifyCalls struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (c *spotifyCalls) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, r)
	c.bodies = append(c.bodies, string(body))
}

func (c *spotifyCalls) last(t *testing.T, path string) (*http.Request, string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.requests) - 1; i >= 0; i-- {
		if c.requests[i].URL.Path == path {
			return c.requests[i], c.bodies[i]
		}
	}
	t.Fatalf("no request to %s", path)
	return &http.Request{}, ""
}

func (c *spotifyCalls) count(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.requests {
		if r.URL.Path == path {
			n++
		}
	}
	return n
}

func spotifyServer(t *testing.T, routes map[string]string, failing map[string]int) (liveSource, *spotifyCalls) {
	t.Helper()
	calls := &spotifyCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.record(r)
		if status, ok := failing[r.URL.Path+"?"+r.URL.Query().Get("totpVer")]; ok {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(routes[r.URL.Path]))
	}))
	t.Cleanup(srv.Close)
	urls := spotifyURLs{site: srv.URL, clientToken: srv.URL + "/v1/clienttoken", pathfinder: srv.URL + "/pathfinder/v2/query"}
	return newSpotifySource(srv.Client(), urls), calls
}

var spotifyRoutes = map[string]string{
	"/api/server-time":     `{"serverTime":1795019639}`,
	"/api/token":           `{"accessToken":"access-1","accessTokenExpirationTimestampMs":1}`,
	"/v1/clienttoken":      `{"granted_token":{"token":"client-1","expires_after_seconds":60}}`,
	"/pathfinder/v2/query": `{"data":{"albumUnion":{}}}`,
	"/oembed":              `{"thumbnail_url":"https://i.scdn.co/image/x"}`,
}

func TestSpotifyPathfinderSendsThePersistedQueryWithTheSessionHeaders(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, nil)
	raw, err := src["album_tracks"](context.Background(), "abc")
	if err != nil || string(raw) != spotifyRoutes["/pathfinder/v2/query"] {
		t.Fatalf("got %s, %v", raw, err)
	}
	req, body := calls.last(t, "/pathfinder/v2/query")
	headers := map[string]string{"Authorization": "Bearer access-1", "client-token": "client-1", "app-platform": "WebPlayer", "Content-Type": "application/json;charset=UTF-8", "User-Agent": spotifyUserAgent}
	for name, want := range headers {
		if got := req.Header.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	want := `{"variables":{"limit":50,"offset":0,"uri":"spotify:album:abc"},"operationName":"queryAlbumTracks","extensions":{"persistedQuery":{"version":1,"sha256Hash":"b9bfabef66ed756e5e13f68a942deb60bd4125ec1f1be8cc42769dc0259b4b10"}}}`
	if req.Method != http.MethodPost || body != want {
		t.Fatalf("%s %s", req.Method, body)
	}
}

func TestSpotifySearchSendsTheAdaptersVariables(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, nil)
	if _, err := src["search"](context.Background(), "Nia Archives"); err != nil {
		t.Fatal(err)
	}
	_, body := calls.last(t, "/pathfinder/v2/query")
	var sent spotifyQuery
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatal(err)
	}
	vars := sent.Variables
	if sent.OperationName != "searchDesktop" || vars["searchTerm"] != "Nia Archives" || vars["limit"] != 10.0 || vars["numberOfTopResults"] != 5.0 {
		t.Fatalf("sent %s", body)
	}
}

func TestSpotifySessionIsResolvedOnceWithTheTOTPFlow(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, nil)
	for _, entity := range []string{"artist_overview", "artist_discography"} {
		if _, err := src[entity](context.Background(), "7BMR0fwtEvzGtK4rNGdoiQ"); err != nil {
			t.Fatal(err)
		}
	}
	token, _ := calls.last(t, "/api/token")
	tokenCalls := calls.count("/api/token")
	_, clientBody := calls.last(t, "/v1/clienttoken")
	clientCalls := calls.count("/v1/clienttoken")
	q := token.URL.Query()
	sixDigits := regexp.MustCompile(`^\d{6}$`)
	if tokenCalls != 1 || clientCalls != 1 || q.Get("totpVer") != "61" || q.Get("reason") != "init" || q.Get("productType") != "web-player" {
		t.Fatalf("token calls %d, client calls %d, query %v", tokenCalls, clientCalls, q)
	}
	if !sixDigits.MatchString(q.Get("totp")) || q.Get("totpServer") != spotifyTOTP(spotifyTOTPSecrets[0].secret, 1795019639) {
		t.Fatalf("totp %q totpServer %q", q.Get("totp"), q.Get("totpServer"))
	}
	if !strings.Contains(clientBody, `"client_id":"`+spotifyWebClientID+`"`) {
		t.Fatalf("client token body %s", clientBody)
	}
}

func TestSpotifyAccessTokenFallsBackToTheNextTOTPVersion(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, map[string]int{"/api/token?61": http.StatusBadRequest})
	if _, err := src["album_tracks"](context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	token, _ := calls.last(t, "/api/token")
	count := calls.count("/api/token")
	if count != 2 || token.URL.Query().Get("totpVer") != "60" {
		t.Fatalf("token calls %d, last version %s", count, token.URL.Query().Get("totpVer"))
	}
}

func TestSpotifyFailsWhenEveryTOTPVersionIsRefused(t *testing.T) {
	failing := map[string]int{"/api/token?61": http.StatusBadRequest, "/api/token?60": http.StatusBadRequest, "/api/token?59": http.StatusBadRequest}
	src, calls := spotifyServer(t, spotifyRoutes, failing)
	if _, err := src["search"](context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("got %v", err)
	}
	if count := calls.count("/pathfinder/v2/query"); count != 0 {
		t.Fatal("queried pathfinder without a session")
	}
}

func TestSpotifyFailsOnAnErrorStatusFromPathfinder(t *testing.T) {
	src, _ := spotifyServer(t, spotifyRoutes, map[string]int{"/pathfinder/v2/query?": http.StatusUnauthorized})
	if _, err := src["artist_overview"](context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("got %v", err)
	}
}

func TestSpotifyFailsWhenTheClientTokenIsMissing(t *testing.T) {
	routes := map[string]string{}
	for path, body := range spotifyRoutes {
		routes[path] = body
	}
	routes["/v1/clienttoken"] = `{"response_type":"RESPONSE_GRANTED_TOKEN_RESPONSE"}`
	src, _ := spotifyServer(t, routes, nil)
	if _, err := src["album_tracks"](context.Background(), "abc"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSpotifyOEmbedAsksForTheOpenSpotifyURL(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, nil)
	if _, err := src["oembed"](context.Background(), "track/7B0gxo0jQCy5Lk93RIODAC"); err != nil {
		t.Fatal(err)
	}
	req, _ := calls.last(t, "/oembed")
	if req.URL.Query().Get("url") != "https://open.spotify.com/track/7B0gxo0jQCy5Lk93RIODAC" || req.Header.Get("User-Agent") != spotifyOEmbedUserAgent {
		t.Fatalf("got %s with %q", req.URL, req.Header.Get("User-Agent"))
	}
	if count := calls.count("/api/token"); count != 0 {
		t.Fatal("oEmbed needs no session")
	}
}

func TestSpotifyRejectsMalformedFixtureIDs(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, nil)
	bad := map[string]string{"oembed": "playlist/x", "content_error": "search|x", "access_token": "5"}
	for entity, id := range bad {
		if _, err := src[entity](context.Background(), id); err == nil {
			t.Errorf("%s %q: want an error", entity, id)
		}
	}
	if len(calls.requests) != 0 {
		t.Fatalf("sent %d requests for malformed ids", len(calls.requests))
	}
}

func TestSpotifyContentErrorRunsTheNamedOperation(t *testing.T) {
	src, calls := spotifyServer(t, spotifyRoutes, nil)
	if _, err := src["content_error"](context.Background(), "artist_overview|notarealid"); err != nil {
		t.Fatal(err)
	}
	_, body := calls.last(t, "/pathfinder/v2/query")
	if !strings.Contains(body, `"operationName":"queryArtistOverview"`) || !strings.Contains(body, `"uri":"spotify:artist:notarealid"`) {
		t.Fatalf("sent %s", body)
	}
}

func TestSpotifyTOTPMatchesAKnownAnswer(t *testing.T) {
	secret := spotifyTOTPSecrets[0].secret
	if got := spotifyTOTP(secret, 1700000000); got != "371599" {
		t.Fatalf("got %s", got)
	}
	if got := spotifyTOTP(secret, 1795019639); got != "453394" {
		t.Fatalf("got %s", got)
	}
}
