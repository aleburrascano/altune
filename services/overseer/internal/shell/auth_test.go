package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

func TestNoRouteSetsCookie(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/config.json", nil),
		httptest.NewRequest(http.MethodGet, "/", nil),
		httptest.NewRequest(http.MethodGet, "/assets/app.js", nil),
		withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)),
		httptest.NewRequest(http.MethodGet, "/api/buckets", nil),
	}
	for _, req := range requests {
		rec := do(srv, req)
		if sc := rec.Header().Values("Set-Cookie"); len(sc) != 0 {
			t.Errorf("%s emitted Set-Cookie %v, want none", req.URL.Path, sc)
		}
	}
}

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

func TestOwnerTokenPasses(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	if rec := do(srv, req); rec.Code != http.StatusOK {
		t.Fatalf("owner GET /api/buckets = %d, want 200", rec.Code)
	}
}

func TestOwnerAccessIsAudited(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	buf := captureSlog(t)

	do(srv, req)

	records := logRecordsNamed(buf, "overseer.auth.access")
	if len(records) != 1 {
		t.Fatalf("owner GET /api/buckets emitted %d access records, want 1", len(records))
	}
	rec := records[0]
	for field, want := range map[string]any{
		"level":   "INFO",
		"subject": ownerID,
		"path":    "/api/buckets",
		"remote":  req.RemoteAddr,
	} {
		if rec[field] != want {
			t.Errorf("access record %s = %v, want %v", field, rec[field], want)
		}
	}
	if rec["time"] == nil || rec["time"] == "" {
		t.Errorf("access record carries no time: %v", rec)
	}
}

func TestAccessAuditCarriesNoTokenMaterial(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets?token="+ownerToken, nil))
	buf := captureSlog(t)

	do(srv, req)

	if logged := buf.String(); strings.Contains(logged, ownerToken) {
		t.Fatalf("token material leaked into the log: %s", logged)
	}
}

func TestDeniedRequestEmitsNoAccessAudit(t *testing.T) {
	srv := newServer(fixedRegistry{})
	req := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
	req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
	buf := captureSlog(t)

	do(srv, req)

	if records := logRecordsNamed(buf, "overseer.auth.access"); len(records) != 0 {
		t.Fatalf("non-owner GET /api/buckets emitted %d access records, want 0", len(records))
	}
}
