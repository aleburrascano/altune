package spotify_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/search/internal/adapters/providers/spotify"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSpotifyAdapter_Search_transportErrorDoesNotLeakTOTP(t *testing.T) {
	var mu sync.Mutex
	var sentCodes []string
	errDialRefused := errors.New("dial tcp: connection refused")

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case serverTimePath:
			rec := httptest.NewRecorder()
			_ = json.NewEncoder(rec).Encode(map[string]int64{"serverTime": fixedServerTime})
			return rec.Result(), nil
		case accessTokenPath:
			q := r.URL.Query()
			mu.Lock()
			sentCodes = append(sentCodes, q.Get("totp"), q.Get("totpServer"))
			mu.Unlock()
			return nil, errDialRefused
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}

	_, err := spotify.NewSpotifyAdapter(client, time.Now).Search(t.Context(), "x", providertest.AllKinds())
	if err == nil {
		t.Fatal("Search() error = nil, want the transport failure")
	}
	if !errors.Is(err, errDialRefused) {
		t.Errorf("Search() error = %v, want it to still wrap the transport cause", err)
	}
	msg := err.Error()
	mu.Lock()
	defer mu.Unlock()
	if len(sentCodes) == 0 {
		t.Fatal("token endpoint never called")
	}
	for _, code := range sentCodes {
		if code == "" {
			t.Fatalf("empty totp code sent; sentCodes = %v", sentCodes)
		}
		if strings.Contains(msg, "="+code) {
			t.Errorf("logged error leaks TOTP code %q: %s", code, msg)
		}
	}
	if strings.Contains(msg, "totp=") || strings.Contains(msg, "totpServer=") {
		t.Errorf("transport error kept the credential query, which providerhttp strips at the source: %s", msg)
	}
	if !strings.Contains(msg, "https://open.spotify.com/api/token") {
		t.Errorf("error lost the failing endpoint for diagnostics: %s", msg)
	}
}

func sentTOTPCodes(t *testing.T, serverTime int64) (local, server string) {
	t.Helper()
	tokens := serveTokens(t, tokenEndpoints{
		serverTime: serverTimeIs(serverTime),
		accessToken: func(w http.ResponseWriter, r *http.Request) {
			local, server = r.URL.Query().Get("totp"), r.URL.Query().Get("totpServer")
			grantAccess("test-access-token", func() int64 { return farFutureExpiryMs })(w, r)
		},
	})
	if err := searchOnce(t, tokens, newTestClock().now); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	return local, server
}

func TestSpotifyAdapter_Search_sendsTOTPCodesForTheLocalAndTheServerClock(t *testing.T) {
	local, server := sentTOTPCodes(t, fixedServerTime)
	if len(local) != 6 || local != server {
		t.Errorf("totp = %q, totpServer = %q, want one six-digit code when both clocks agree", local, server)
	}
	local, server = sentTOTPCodes(t, fixedServerTime+3600)
	if len(local) != 6 || len(server) != 6 || local == server {
		t.Errorf("totp = %q, totpServer = %q, want two six-digit codes for clocks an hour apart", local, server)
	}
}

func TestSpotifyAdapter_Search_rejectsAnOversizedClientTokenBody(t *testing.T) {
	oversized := `{"granted_token":{"token":"` + strings.Repeat("x", providerrequests.BodyCap+1024) + `"}}`
	tokens := serveTokens(t, tokenEndpoints{clientToken: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oversized))
	}})
	var sentClientTokenLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentClientTokenLen = len(r.Header.Get("client-token"))
		_, _ = w.Write([]byte(spotifyFixtureResponse))
	}))
	defer srv.Close()

	_, err := newSpotifyAdapterWithTokens(tokens, srv, time.Now).Search(t.Context(), "x", providertest.AllKinds())
	if err == nil {
		t.Fatalf("Search took a client token off a %d-byte body; want the body capped at %d and the decode rejected",
			len(oversized), providerrequests.BodyCap)
	}
	if sentClientTokenLen > 0 {
		t.Errorf("pathfinder got a %d-byte client token, want no search sent without a decoded token", sentClientTokenLen)
	}
}

func TestSpotifyAdapter_Search_clientTokenStatusReachesBreaker(t *testing.T) {
	tokens := serveTokens(t, tokenEndpoints{clientToken: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}})

	_, err := newSpotifyAdapterWithTokens(tokens, servePathfinder(t, spotifyFixtureResponse), time.Now).
		Search(t.Context(), "x", providertest.AllKinds())
	if err == nil {
		t.Fatal("Search() error = nil, want the client-token 503")
	}
	var status interface{ UpstreamStatus() int }
	if !errors.As(err, &status) {
		t.Fatalf("Search() error = %v, want an error carrying UpstreamStatus() for the breaker", err)
	}
	if got := status.UpstreamStatus(); got != http.StatusServiceUnavailable {
		t.Errorf("UpstreamStatus() = %d, want 503", got)
	}
}

func upstreamTokens(t *testing.T, serverTime int64, accessExpiryMs func() int64, clientSeconds int64) *tokenServer {
	t.Helper()
	return serveTokens(t, tokenEndpoints{
		serverTime:  serverTimeIs(serverTime),
		accessToken: grantAccess("resolved-token", accessExpiryMs),
		clientToken: grantClient("client-token", clientSeconds),
	})
}

func searchOnce(t *testing.T, tokens *tokenServer, now func() time.Time) error {
	t.Helper()
	a := newSpotifyAdapterWithTokens(tokens, servePathfinder(t, spotifyFixtureResponse), now)
	_, err := a.Search(t.Context(), "x", providertest.AllKinds())
	return err
}

func TestSpotifyAdapter_Search_rejectsBadAccessExpiry(t *testing.T) {
	clock := newTestClock()
	for name, expiry := range map[string]func() int64{
		"zero":     func() int64 { return 0 },
		"negative": func() int64 { return -5 },
		"past":     clock.inMillis(-time.Hour),
		"now":      clock.inMillis(0),
	} {
		t.Run(name, func(t *testing.T) {
			if err := searchOnce(t, upstreamTokens(t, fixedServerTime, expiry, 1209600), clock.now); err == nil {
				t.Fatal("Search() error = nil, want a rejected access-token expiry")
			}
		})
	}
}

func TestSpotifyAdapter_Search_rejectsBadClientLifetime(t *testing.T) {
	clock := newTestClock()
	for name, seconds := range map[string]int64{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			if err := searchOnce(t, upstreamTokens(t, fixedServerTime, clock.inMillis(time.Hour), seconds), clock.now); err == nil {
				t.Fatal("Search() error = nil, want a rejected client-token lifetime")
			}
		})
	}
}

func TestSpotifyAdapter_Search_rejectsNonPositiveServerTime(t *testing.T) {
	clock := newTestClock()
	for _, serverTime := range []int64{0, -1} {
		if err := searchOnce(t, upstreamTokens(t, serverTime, clock.inMillis(time.Hour), 1209600), clock.now); err == nil {
			t.Errorf("Search() error = nil for serverTime %d, want rejection", serverTime)
		}
	}
}

func mustSearch(t *testing.T, a *spotify.SpotifyAdapter) {
	t.Helper()
	if _, err := a.Search(t.Context(), "x", providertest.AllKinds()); err != nil {
		t.Fatalf("Search() error = %v, want a session", err)
	}
}

func TestSpotifyAdapter_Search_keepsASessionUsableForShortOrHugeLifetimesButNeverPast24h(t *testing.T) {
	const shortUsable, shortExpired = 29 * time.Second, 31 * time.Second
	const longUsable, longExpired = 22 * time.Hour, 24*time.Hour + time.Second
	farFuture := func(*testClock) func() int64 { return func() int64 { return farFutureExpiryMs } }
	inAnHour := func(c *testClock) func() int64 { return c.inMillis(time.Hour) }
	cases := map[string]struct {
		accessExpiry        func(*testClock) func() int64
		clientSeconds       int64
		usableFor, deadFrom time.Duration
	}{
		"short client lifetime": {inAnHour, 60, shortUsable, shortExpired},
		"short access lifetime": {func(c *testClock) func() int64 { return c.inMillis(10 * time.Second) }, 1209600, shortUsable, shortExpired},
		"long lifetimes":        {farFuture, longClientLifetimeSec, longUsable, longExpired},
		"overflowing client":    {farFuture, math.MaxInt64, longUsable, longExpired},
		"overflowing access":    {func(*testClock) func() int64 { return func() int64 { return math.MaxInt64 } }, longClientLifetimeSec, longUsable, longExpired},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			clock := newTestClock()
			tokens := upstreamTokens(t, fixedServerTime, c.accessExpiry(clock), c.clientSeconds)
			a := newSpotifyAdapterWithTokens(tokens, servePathfinder(t, spotifyFixtureResponse), clock.now)

			mustSearch(t, a)
			clock.advance(c.usableFor)
			mustSearch(t, a)
			if got := tokens.resolves.Load(); got != 1 {
				t.Errorf("session resolved %d times %v after the first, want 1 (it is still usable)", got, c.usableFor)
			}
			clock.advance(c.deadFrom - c.usableFor)
			mustSearch(t, a)
			if got := tokens.resolves.Load(); got != 2 {
				t.Errorf("session resolved %d times %v after the first, want 2 (it has expired)", got, c.deadFrom)
			}
		})
	}
}

func TestSpotifyAdapter_Search_reResolvesTheSessionOnceTheAccessTokenExpires(t *testing.T) {
	clock := newTestClock()
	tokens := serveTokens(t, tokenEndpoints{
		accessToken: grantAccessInTurn(clock.inMillis(time.Hour), "first-token", "fresh-token"),
		clientToken: grantClient("client-token", longClientLifetimeSec),
	})
	var lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(spotifyFixtureResponse))
	}))
	defer srv.Close()
	a := newSpotifyAdapterWithTokens(tokens, srv, clock.now)

	mustSearch(t, a)
	clock.advance(58 * time.Minute)
	mustSearch(t, a)
	if lastAuth != "Bearer first-token" {
		t.Fatalf("Authorization before the access expiry margin = %q, want the cached first-token", lastAuth)
	}
	clock.advance(2 * time.Minute)
	mustSearch(t, a)
	if lastAuth != "Bearer fresh-token" {
		t.Errorf("Authorization after the access token expired = %q, want the re-resolved fresh-token", lastAuth)
	}
}
