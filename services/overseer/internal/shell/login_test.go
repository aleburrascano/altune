package shell_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// findCookie returns the Set-Cookie value for the named cookie, or nil.
func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func postLogin(srv http.Handler, token string) *httptest.ResponseRecorder {
	form := url.Values{"token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// GET /login serves the token form without auth and never leaks a token.
func TestLoginFormRenders(t *testing.T) {
	srv := newTestServer(testToken)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/login"`) || !strings.Contains(body, `name="token"`) {
		t.Errorf("login form missing its POST target or token field:\n%s", body)
	}
	if strings.Contains(body, testToken) {
		t.Error("login form echoed the owner token")
	}
	// The token-entry page carries the same hardening as the data page.
	for h, want := range map[string]string{
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
	} {
		if got := rec.Header().Get(h); got != want {
			t.Errorf("login header %s = %q, want %q", h, got, want)
		}
	}
}

// A correct token sets the owner cookie with all required flags and 303s to /.
func TestLoginSuccessSetsCookieAndRedirects(t *testing.T) {
	srv := newTestServer(testToken)
	rec := postLogin(srv, testToken)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login (good) = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("redirect Location = %q, want %q", loc, "/")
	}
	if strings.Contains(rec.Header().Get("Location"), testToken) {
		t.Error("token leaked into redirect URL")
	}

	c := findCookie(rec, "overseer_token")
	if c == nil {
		t.Fatal("no overseer_token cookie set on successful login")
	}
	if c.Value != testToken {
		t.Errorf("cookie value = %q, want the owner token", c.Value)
	}
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if !c.Secure {
		t.Error("cookie must be Secure")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("cookie Path = %q, want /", c.Path)
	}

	// The cookie it just set must actually unlock the shell.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET / with login cookie = %d, want 200", rec2.Code)
	}
}

// A wrong token — including one the same length as the real one, so the guard is
// not a prefix match — sets no cookie, grants no access, and echoes nothing.
func TestLoginRejectsBadToken(t *testing.T) {
	srv := newTestServer(testToken)
	sameLenWrong := strings.Repeat("x", len(testToken))
	for _, bad := range []string{"wrong", "", testToken[:len(testToken)-1], sameLenWrong} {
		rec := postLogin(srv, bad)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST /login (%q) = %d, want 401", bad, rec.Code)
		}
		if c := findCookie(rec, "overseer_token"); c != nil {
			t.Errorf("POST /login (%q) set a cookie on failure: %v", bad, c)
		}
		if strings.Contains(rec.Body.String(), testToken) {
			t.Errorf("POST /login (%q) echoed the owner token", bad)
		}
	}
}

// Fail closed: with no owner token configured, even a matching empty submission is
// rejected and sets no cookie.
func TestLoginFailsClosedWithoutConfiguredToken(t *testing.T) {
	srv := newTestServer("")
	rec := postLogin(srv, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /login on empty-token server = %d, want 401", rec.Code)
	}
	if c := findCookie(rec, "overseer_token"); c != nil {
		t.Errorf("empty-token server set a cookie: %v", c)
	}
}

// Under a mount prefix every OUTBOUND path carries the base: the form action, the
// post-login redirect and the cookie Path all sit inside /overseer, while inbound
// routes stay unprefixed (Caddy strips the prefix). The cookie the login sets must
// still unlock the shell.
func TestLoginBasePathThreadsOutboundPaths(t *testing.T) {
	srv := newTestServerWithBase(testToken, "/overseer")

	// Form action carries the base.
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `action="/overseer/login"`) {
		t.Errorf("login form action missing base prefix:\n%s", body)
	}

	// Successful POST redirects under the base and scopes the cookie to the base.
	rec = postLogin(srv, testToken)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login (good) = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/overseer/" {
		t.Fatalf("post-login redirect Location = %q, want /overseer/", loc)
	}
	c := findCookie(rec, "overseer_token")
	if c == nil {
		t.Fatal("no overseer_token cookie set on successful login")
	}
	if c.Path != "/overseer" {
		t.Errorf("cookie Path = %q, want /overseer", c.Path)
	}

	// The cookie unlocks the (inbound, unprefixed) shell route.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET / with login cookie = %d, want 200", rec2.Code)
	}
}

// Rootless default: an empty base reproduces today's exact outbound paths — bare
// /login action, / redirect, cookie Path / — byte-for-byte.
func TestLoginEmptyBaseUnchanged(t *testing.T) {
	srv := newTestServerWithBase(testToken, "")

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `action="/login"`) {
		t.Errorf("empty-base form action changed:\n%s", body)
	}

	rec = postLogin(srv, testToken)
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("empty-base post-login redirect = %q, want /", loc)
	}
	if c := findCookie(rec, "overseer_token"); c == nil || c.Path != "/" {
		t.Fatalf("empty-base cookie Path = %v, want /", c)
	}
}

// A browser navigating to the shell without a token is redirected to /login...
func TestUnauthBrowserRedirectsToLogin(t *testing.T) {
	srv := newTestServer(testToken)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth browser GET / = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("redirect Location = %q, want /login", loc)
	}
}

// ...but an API/curl client keeps getting the bare 401, never a redirect, whether
// it sends no Accept, a non-HTML Accept, or a bad bearer token.
func TestUnauthAPIStillGets401(t *testing.T) {
	srv := newTestServer(testToken)
	cases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"no accept", func(*http.Request) {}},
		{"json accept", func(r *http.Request) { r.Header.Set("Accept", "application/json") }},
		{"star accept", func(r *http.Request) { r.Header.Set("Accept", "*/*") }},
		{"bad bearer even with html accept", func(r *http.Request) {
			r.Header.Set("Accept", "text/html")
			r.Header.Set("Authorization", "Bearer wrong")
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
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Fatalf("%s: got redirect to %q, want none", tc.name, loc)
			}
		})
	}
}
