package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Must-hold (single owner only): a valid NON-OWNER Supabase token is rejected with
// 403 — only the allowlisted user id passes. No RBAC.
func TestNonOwnerTokenForbidden(t *testing.T) {
	srv := newServer(fixedRegistry{})
	for _, path := range []string{"/api/buckets", "/api/stream"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		if rec := do(srv, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s with non-owner token = %d, want 403", path, rec.Code)
		}
	}
}

// Must-hold (missing/invalid -> 401): no token, a malformed header, a bad scheme,
// a token-in-query, and an unverifiable token are all 401 on a data route.
func TestMissingOrInvalidTokenUnauthorized(t *testing.T) {
	srv := newServer(fixedRegistry{})
	cases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"no header", func(*http.Request) {}},
		{"empty bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }},
		{"no scheme", func(r *http.Request) { r.Header.Set("Authorization", ownerToken) }},
		{"basic scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+ownerToken) }},
		{"token as query", func(r *http.Request) { r.URL.RawQuery = "token=" + ownerToken }},
		{"unverifiable token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer forged.jwt.value") }},
		{"cookie is not accepted", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "overseer_token", Value: ownerToken}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
			tc.mutate(req)
			if rec := do(srv, req); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: got %d, want 401", tc.name, rec.Code)
			}
		})
	}
}

// Must-hold (no cookie): NO overseer route emits Set-Cookie — auth is bearer only.
func TestNoRouteSetsCookie(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/config.json", nil),
		httptest.NewRequest(http.MethodGet, "/", nil),
		httptest.NewRequest(http.MethodGet, "/assets/app.js", nil),
		withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)),
		// A rejected request must not set a cookie either.
		httptest.NewRequest(http.MethodGet, "/api/buckets", nil),
	}
	for _, req := range requests {
		rec := do(srv, req)
		if sc := rec.Header().Values("Set-Cookie"); len(sc) != 0 {
			t.Errorf("%s emitted Set-Cookie %v, want none", req.URL.Path, sc)
		}
	}
}

// Fail closed: with no owner id configured, even a verifiable token is rejected —
// every subject mismatches "".
func TestFailsClosedWithoutOwnerID(t *testing.T) {
	srv := shell.NewHandler(fixedRegistry{},
		shell.WithVerifier(newFakeVerifier()),
		shell.WithOwnerUserID(""),
		shell.WithStreamInterval(10*time.Millisecond),
	).Router()
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	if rec := do(srv, req); rec.Code != http.StatusForbidden {
		t.Fatalf("no-owner-id server = %d, want 403 (fail closed)", rec.Code)
	}
}

// The owner token passes the guard onto a data route.
func TestOwnerTokenPasses(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	if rec := do(srv, req); rec.Code != http.StatusOK {
		t.Fatalf("owner GET /api/buckets = %d, want 200", rec.Code)
	}
}
