package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/shared/logging"
)

func TestCorrelationID_SetsHeader(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	corrID := rec.Header().Get("X-Correlation-ID")
	if corrID == "" {
		t.Fatal("expected X-Correlation-ID header to be set, got empty")
	}
	if len(corrID) != 8 {
		t.Errorf("X-Correlation-ID length = %d, want 8 (uuid[:8])", len(corrID))
	}
}

func TestCorrelationID_PropagatesInContext(t *testing.T) {
	var capturedID string
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetCorrelationID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Fatal("expected correlation ID in context, got empty")
	}
	headerID := rec.Header().Get("X-Correlation-ID")
	if capturedID != headerID {
		t.Errorf("context ID %q does not match header ID %q", capturedID, headerID)
	}
}

func TestCorrelationID_UniqueBetweenRequests(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	rec2 := httptest.NewRecorder()

	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/a", nil))
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/b", nil))

	id1 := rec1.Header().Get("X-Correlation-ID")
	id2 := rec2.Header().Get("X-Correlation-ID")
	if id1 == id2 {
		t.Errorf("expected unique correlation IDs between requests, both are %q", id1)
	}
}

func TestGetCorrelationID_EmptyContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	id := GetCorrelationID(req.Context())

	if id != "" {
		t.Errorf("expected empty string for context without correlation ID, got %q", id)
	}
}

func TestCorrelationID_AdoptsInboundHeader(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	const inbound = "trace-abc12345"
	var capturedID string
	handler := CorrelationID(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetCorrelationID(r.Context())
		w.WriteHeader(http.StatusOK)
	})))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Correlation-ID", inbound)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedID != inbound {
		t.Errorf("context ID = %q, want inbound %q", capturedID, inbound)
	}
	if got := rec.Header().Get("X-Correlation-ID"); got != inbound {
		t.Errorf("response header = %q, want inbound %q", got, inbound)
	}
	sawStart := false
	for _, r := range ring.Snapshot() {
		if r.Message == "request.start" {
			sawStart = true
			if r.Attrs["corr_id"] != inbound {
				t.Errorf("log corr_id = %q, want inbound %q", r.Attrs["corr_id"], inbound)
			}
		}
	}
	if !sawStart {
		t.Fatal("expected request.start log line so the corr_id assertion is meaningful")
	}
}

func TestCorrelationID_MintsWhenHeaderAbsent(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Correlation-ID")
	if len(id) != 8 {
		t.Errorf("minted id length = %d, want 8 (uuid[:8]); got %q", len(id), id)
	}
}

func TestCorrelationID_MintsWhenHeaderMalformed(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Correlation-ID", "bad id\nwith spaces")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Correlation-ID"); got == "bad id\nwith spaces" || len(got) != 8 {
		t.Errorf("expected minted 8-char id for malformed inbound header, got %q", got)
	}
}

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

func TestRecoverer_CatchesPanic_Returns500(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went very wrong")
	}))
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body.Detail != "internal server error" {
		t.Errorf("detail = %q, want %q", body.Detail, "internal server error")
	}
}

func TestRecoverer_NoPanic_PassesThrough(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fine"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "fine" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fine")
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
