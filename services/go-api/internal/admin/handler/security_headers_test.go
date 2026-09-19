package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSupabaseURL = "https://proj.supabase.co"

func serveIndexWithSupabase(t *testing.T, supabaseURL string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	New(nil, nil).WithSupabaseLogin(supabaseURL, "anon-key").
		ServeIndex(rec, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	return rec
}

// The console holds the operator's Supabase tokens in sessionStorage and builds
// its DOM from ~56 innerHTML templates, so a framing page or a missed esc() is
// a path to that token. These headers are what close both (#1995).
func TestServeIndex_SendsFramingAndContentPolicyHeaders(t *testing.T) {
	rec := serveIndexWithSupabase(t, testSupabaseURL)

	want := map[string]string{
		"Content-Type":    "text/html; charset=utf-8",
		"X-Frame-Options": "DENY",
		"Referrer-Policy": "no-referrer",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
	policy := rec.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'self'",
		"frame-ancestors 'none'",
		"connect-src 'self' " + testSupabaseURL,
		"script-src 'self' 'unsafe-inline'",
	} {
		if !strings.Contains(policy, directive) {
			t.Errorf("Content-Security-Policy = %q, want it to carry %q", policy, directive)
		}
	}
}

// Artwork comes from provider-owned https hosts, so an img-src the page cannot
// satisfy would blank every row rather than fail loudly.
func TestServeIndex_ContentPolicyAllowsProviderArtwork(t *testing.T) {
	rec := serveIndexWithSupabase(t, testSupabaseURL)

	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "img-src 'self' https:") {
		t.Errorf("Content-Security-Policy = %q, want provider https artwork allowed", rec.Header().Get("Content-Security-Policy"))
	}
}

// url.Parse admits a semicolon and an apostrophe inside a host, so a configured
// URL carrying either would otherwise end the connect-src source list and start
// a directive of its author's choosing.
func TestServeIndex_ContentPolicyDropsAnUnusableSupabaseURL(t *testing.T) {
	for _, supabaseURL := range []string{
		"https://proj.supabase.co;script-src",
		"https://proj.supabase.co'",
		"https://proj.supabase.co ftp:",
		"not-a-url",
		"",
	} {
		rec := serveIndexWithSupabase(t, supabaseURL)

		policy := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(policy, "connect-src 'self';") {
			t.Errorf("supabase url %q: policy = %q, want connect-src reduced to 'self'", supabaseURL, policy)
		}
		if strings.Contains(policy, "script-src *") || strings.Contains(policy, "ftp:") {
			t.Errorf("supabase url %q: policy = %q, want the configured value not to reach the policy", supabaseURL, policy)
		}
	}
}

func recordThrough(middleware func(http.Handler) http.Handler, next http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	middleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/requests", nil))
	return rec
}

// An authenticated admin payload sitting in a shared cache is readable by the
// next principal through that cache.
func TestNoStoreAndNosniff_SetsBothOnADataResponse(t *testing.T) {
	rec := recordThrough(NoStoreAndNosniff, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// no-store on an SSE response ends the stream in some proxies, so the stream
// handlers keep their own Cache-Control and only inherit nosniff.
func TestNoStoreAndNosniff_LeavesTheStreamCacheHeaderInPlace(t *testing.T) {
	rec := recordThrough(NoStoreAndNosniff, func(w http.ResponseWriter, _ *http.Request) {
		setStreamHeaders(w)
		w.WriteHeader(http.StatusOK)
	})

	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want the stream's own no-cache", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
