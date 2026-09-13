package httputil

import (
	"altune/go-api/internal/shared/logging"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogger_DoesNotPanic(t *testing.T) {
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequestLogger_TracksStatusCode(t *testing.T) {
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRequestLogger_RawQueryAbsentFromRing(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	const secret = "queenssecretobsessionquery"
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/discovery/search?q="+secret+"&limit=5", nil)

	handler.ServeHTTP(httptest.NewRecorder(), req)

	sawRequestStart := false
	for _, rec := range ring.Snapshot() {
		if rec.Message == "request.start" {
			sawRequestStart = true
		}
		if strings.Contains(rec.Message, secret) {
			t.Errorf("raw query leaked into ring message: %q", rec.Message)
		}
		for k, v := range rec.Attrs {
			if strings.Contains(v, secret) {
				t.Errorf("raw query leaked into ring attr %q = %q", k, v)
			}
		}
	}
	if !sawRequestStart {
		t.Fatal("expected request.start to be captured, so the redaction assertion is meaningful")
	}
}

func TestStatusWriter_DefaultStatus200(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	n, err := sw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("bytes written = %d, want 5", n)
	}
	if sw.bytes != 5 {
		t.Errorf("sw.bytes = %d, want 5", sw.bytes)
	}
	if sw.status != 200 {
		t.Errorf("sw.status = %d, want 200 (default)", sw.status)
	}
}

func TestStatusWriter_TracksWriteHeaderAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	sw.WriteHeader(http.StatusCreated)
	sw.Write([]byte("abc"))
	sw.Write([]byte("de"))

	if sw.status != http.StatusCreated {
		t.Errorf("sw.status = %d, want %d", sw.status, http.StatusCreated)
	}
	if sw.bytes != 5 {
		t.Errorf("sw.bytes = %d, want 5 (3+2)", sw.bytes)
	}
}
