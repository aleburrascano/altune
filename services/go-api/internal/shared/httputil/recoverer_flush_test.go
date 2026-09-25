package httputil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverer_PanicAfterFlushKeepsTheImplicit200(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		panic("mid-stream failure")
	}))
	rec := &headerCountingRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sse", nil))

	if rec.writeHeaderCalls > 1 {
		t.Errorf("WriteHeader called %d times, want at most once", rec.writeHeaderCalls)
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("body = %q, want no error body appended to the stream", got)
	}
}
