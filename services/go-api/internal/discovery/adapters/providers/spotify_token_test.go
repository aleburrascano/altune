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
