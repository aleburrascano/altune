package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testToken = "test-owner-token-0123456789abcdef" // 33 chars, >= min

type emptyRegistry struct{}

func (emptyRegistry) Buckets() []core.Bucket { return nil }

func newTestServer(token string) http.Handler {
	return shell.NewHandler(emptyRegistry{}).Router(token)
}

// Spine invariant: owner-only. An unauthenticated request to Overseer data is
// rejected. /health stays open (uptime backstop), the shell page does not.
func TestShellRejectsUnauthenticated(t *testing.T) {
	srv := newTestServer(testToken)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET / = %d, want 401", rec.Code)
	}
}

func TestHealthIsOpen(t *testing.T) {
	srv := newTestServer(testToken)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", rec.Code)
	}
}

func TestShellAcceptsBearerToken(t *testing.T) {
	srv := newTestServer(testToken)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated GET / = %d, want 200", rec.Code)
	}
}

func TestShellAcceptsCookieToken(t *testing.T) {
	srv := newTestServer(testToken)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "overseer_token", Value: testToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie-authed GET / = %d, want 200", rec.Code)
	}
}

// Hostile inputs and abused authority: none of these may pass the guard.
func TestShellRejectsHostileTokens(t *testing.T) {
	srv := newTestServer(testToken)
	cases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"wrong bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }},
		{"empty bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }},
		{"no scheme", func(r *http.Request) { r.Header.Set("Authorization", testToken) }},
		{"basic scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+testToken) }},
		{"wrong cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "overseer_token", Value: "nope"}) }},
		{"token as query", func(r *http.Request) { r.URL.RawQuery = "token=" + testToken }},
		{"prefix of token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+testToken[:len(testToken)-1]) }},
		{"header wins over good cookie", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer wrong")
			r.AddCookie(&http.Cookie{Name: "overseer_token", Value: testToken})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.mutate(req)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: got %d, want 401", tc.name, rec.Code)
			}
		})
	}
}

// Fail closed: with no owner token configured, every data request is rejected.
func TestShellFailsClosedWithoutToken(t *testing.T) {
	srv := newTestServer("")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty-token server GET / = %d, want 401", rec.Code)
	}
}
