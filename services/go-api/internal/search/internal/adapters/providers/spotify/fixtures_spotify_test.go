package spotify_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/search/internal/adapters/providers/spotify"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	pathfinderHost        = "api-partner.spotify.com"
	serverTimePath        = "/api/server-time"
	accessTokenPath       = "/api/token"
	clientTokenPath       = "/v1/clienttoken"
	fixedServerTime       = 1700000000
	farFutureExpiryMs     = 99999999999999
	longClientLifetimeSec = 999999
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Unix(fixedServerTime, 0)} }

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *testClock) inMillis(d time.Duration) func() int64 {
	return func() int64 { return c.now().Add(d).UnixMilli() }
}

type tokenEndpoints struct {
	serverTime  http.HandlerFunc
	accessToken http.HandlerFunc
	clientToken http.HandlerFunc
}

type tokenServer struct {
	*httptest.Server
	resolves atomic.Int32
}

func serveTokens(t *testing.T, e tokenEndpoints) *tokenServer {
	t.Helper()
	if e.serverTime == nil {
		e.serverTime = serverTimeIs(fixedServerTime)
	}
	if e.accessToken == nil {
		e.accessToken = grantAccess("test-access-token", func() int64 { return farFutureExpiryMs })
	}
	if e.clientToken == nil {
		e.clientToken = grantClient("test-client-token", longClientLifetimeSec)
	}
	ts := &tokenServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case serverTimePath:
			ts.resolves.Add(1)
			e.serverTime(w, r)
		case accessTokenPath:
			e.accessToken(w, r)
		case clientTokenPath:
			e.clientToken(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func writeJSON(w http.ResponseWriter, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

func serverTimeIs(serverTime int64) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]int64{"serverTime": serverTime})
	}
}

func grantAccess(token string, expiryMs func() int64) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"accessToken": token, "accessTokenExpirationTimestampMs": expiryMs()})
	}
}

func grantAccessInTurn(expiryMs func() int64, tokens ...string) http.HandlerFunc {
	var n atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		i := min(int(n.Add(1))-1, len(tokens)-1)
		grantAccess(tokens[i], expiryMs)(w, r)
	}
}

func farFutureExpiry() int64 { return farFutureExpiryMs }

func grantClient(token string, lifetimeSeconds int64) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"granted_token": map[string]any{"token": token, "expires_after_seconds": lifetimeSeconds},
		})
	}
}

func spotifyClient(tokens, pathfinder *httptest.Server) *http.Client {
	return &http.Client{Transport: providertest.FakeRoundTripper{Fn: func(r *http.Request) (*http.Response, error) {
		target := tokens
		if r.URL.Host == pathfinderHost {
			target = pathfinder
		}
		return (&providertest.RedirectTransport{TargetURL: target.URL}).RoundTrip(r)
	}}}
}

func newSpotifyAdapterWithTokens(tokens *tokenServer, pathfinder *httptest.Server, now func() time.Time) *spotify.SpotifyAdapter {
	return spotify.NewSpotifyAdapter(spotifyClient(tokens.Server, pathfinder), now)
}

func newTestSpotifyAdapter(t *testing.T, pathfinder *httptest.Server) *spotify.SpotifyAdapter {
	t.Helper()
	return newSpotifyAdapterWithTokens(serveTokens(t, tokenEndpoints{}), pathfinder, time.Now)
}

func servePathfinder(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}
