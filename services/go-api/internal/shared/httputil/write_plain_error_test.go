package httputil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWritePlainError_MatchesHttpErrorOutput(t *testing.T) {
	rec := httptest.NewRecorder()
	WritePlainError(rec, http.StatusTooManyRequests, "too many event streams")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
	if got := rec.Body.String(); got != "too many event streams\n" {
		t.Fatalf("body = %q", got)
	}
}
