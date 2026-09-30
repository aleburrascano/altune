package providers

import (
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
		case "/server-time":
			rec := httptest.NewRecorder()
			_ = json.NewEncoder(rec).Encode(map[string]int64{"serverTime": 1700000000})
			return rec.Result(), nil
		case "/token":
			q := r.URL.Query()
			mu.Lock()
			sentCodes = append(sentCodes, q.Get("totp"), q.Get("totpServer"))
			mu.Unlock()
			return nil, errDialRefused
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}

	a := NewSpotifyAdapter(client)
	a.resolver.serverTimeURL = "http://spotify.test/server-time"
	a.resolver.accessTokenURL = "http://spotify.test/token"
	a.resolver.clientTokenURL = "http://spotify.test/clienttoken"

	_, err := a.Search(t.Context(), "x", allKinds())
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
	if !strings.Contains(msg, "http://spotify.test/token") {
		t.Errorf("error lost the failing endpoint for diagnostics: %s", msg)
	}
}

func spotifyTokenResolverServing(t *testing.T, clientToken http.HandlerFunc) *spotifyTokenResolver {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-time":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": 1700000000})
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":                      "resolved-token",
				"accessTokenExpirationTimestampMs": 99999999999999,
			})
		case "/clienttoken":
			clientToken(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	r := newSpotifyTokenResolver(srv.Client())
	r.serverTimeURL = srv.URL + "/server-time"
	r.accessTokenURL = srv.URL + "/token"
	r.clientTokenURL = srv.URL + "/clienttoken"
	return r
}

func TestSpotifyTokenResolver_clientTokenOversizedBodyIsRejected(t *testing.T) {
	oversized := `{"granted_token":{"token":"` + strings.Repeat("x", providerBodyCap+1024) + `"}}`
	r := spotifyTokenResolverServing(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oversized))
	})

	token, _, err := r.resolveClientToken(t.Context())
	if err == nil {
		t.Fatalf("resolveClientToken took a %d-byte token off a %d-byte body; want the body capped at %d and the decode rejected",
			len(token), len(oversized), providerBodyCap)
	}
	if len(token) > providerBodyCap {
		t.Errorf("len(token) = %d, want nothing beyond the %d-byte cap buffered", len(token), providerBodyCap)
	}
}

func TestSpotifyTokenResolver_clientTokenStatusReachesBreaker(t *testing.T) {
	r := spotifyTokenResolverServing(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := r.get(t.Context())
	if err == nil {
		t.Fatal("get() error = nil, want the client-token 503")
	}
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) {
		t.Fatalf("get() error = %v, want an error carrying HTTPStatus() for the breaker", err)
	}
	if got := status.HTTPStatus(); got != http.StatusServiceUnavailable {
		t.Errorf("HTTPStatus() = %d, want 503", got)
	}
}

func spotifyTokenResolverWithUpstream(t *testing.T, serverTime int64, accessExpiryMs func() int64, clientSeconds int64) *spotifyTokenResolver {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/server-time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverTime})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":                      "resolved-token",
				"accessTokenExpirationTimestampMs": accessExpiryMs(),
			})
		case "/clienttoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"granted_token": map[string]any{"token": "client-token", "expires_after_seconds": clientSeconds},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	r := newSpotifyTokenResolver(srv.Client())
	r.serverTimeURL = srv.URL + "/server-time"
	r.accessTokenURL = srv.URL + "/token"
	r.clientTokenURL = srv.URL + "/clienttoken"
	return r
}

func inMillis(d time.Duration) func() int64 {
	return func() int64 { return time.Now().Add(d).UnixMilli() }
}

func TestSpotifyTokenResolver_resolveRejectsBadAccessExpiry(t *testing.T) {
	for name, expiry := range map[string]func() int64{
		"zero":     func() int64 { return 0 },
		"negative": func() int64 { return -5 },
		"past":     inMillis(-time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			r := spotifyTokenResolverWithUpstream(t, 1700000000, expiry, 1209600)
			if _, _, err := r.resolve(t.Context()); err == nil {
				t.Fatal("resolve() error = nil, want a rejected access-token expiry")
			}
		})
	}
}

func TestSpotifyTokenResolver_resolveRejectsBadClientLifetime(t *testing.T) {
	for name, seconds := range map[string]int64{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			r := spotifyTokenResolverWithUpstream(t, 1700000000, inMillis(time.Hour), seconds)
			if _, _, err := r.resolve(t.Context()); err == nil {
				t.Fatal("resolve() error = nil, want a rejected client-token lifetime")
			}
		})
	}
}

func TestSpotifyTokenResolver_resolveKeepsSessionUsableForShortOrHugeLifetimes(t *testing.T) {
	cases := map[string]struct {
		accessExpiry  func() int64
		clientSeconds int64
	}{
		"short client lifetime": {inMillis(time.Hour), 60},
		"short access lifetime": {inMillis(10 * time.Second), 1209600},
		"overflowing client":    {inMillis(time.Hour), math.MaxInt64},
		"overflowing access":    {func() int64 { return math.MaxInt64 }, 1209600},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := spotifyTokenResolverWithUpstream(t, 1700000000, c.accessExpiry, c.clientSeconds)
			session, _, err := r.resolve(t.Context())
			if err != nil {
				t.Fatalf("resolve() error = %v, want a session", err)
			}
			if !session.valid() {
				t.Error("session.valid() = false, want a freshly resolved session usable")
			}
			if limit := time.Now().Add(25 * time.Hour); session.accessExpiry.After(limit) || session.clientExpiry.After(limit) {
				t.Errorf("expiries %v / %v exceed the 24h ceiling", session.accessExpiry, session.clientExpiry)
			}
		})
	}
}

func TestSpotifyTokenResolver_fetchServerTimeRejectsNonPositive(t *testing.T) {
	for _, serverTime := range []int64{0, -1} {
		r := spotifyTokenResolverWithUpstream(t, serverTime, inMillis(time.Hour), 1209600)
		if _, err := r.fetchServerTime(t.Context()); err == nil {
			t.Errorf("fetchServerTime() error = nil for serverTime %d, want rejection", serverTime)
		}
	}
}
