package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func recordThrough(middleware func(http.Handler) http.Handler, next http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	middleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/health", nil))
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
