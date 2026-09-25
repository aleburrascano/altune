package providers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestSpotifyAdapter_Search_transportErrorDoesNotLeakTOTP forces a network-level
// failure on the TOTP access-token request. Go's http.Client wraps it in a
// *url.Error that embeds the full request URL; the error that reaches the
// fan-out WARN log must not carry the totp/totpServer values.
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

// spotifyTokenResolverServing builds a resolver whose server-time and
// access-token endpoints answer normally, so a test drives the client-token
// call alone.
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

// TestSpotifyTokenResolver_clientTokenStatusReachesBreaker pins the typed
// status: the discovery circuit breaker reads HTTPStatus() to tell an unhealthy
// Spotify from a rejection of this one request, so the status has to survive
// the session-resolve wraps.
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
