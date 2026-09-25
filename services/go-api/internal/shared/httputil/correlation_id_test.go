package httputil

import (
	"altune/go-api/internal/shared/logging"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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
		capturedID = logging.CorrelationIDFromContext(r.Context())
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

	id := logging.CorrelationIDFromContext(req.Context())

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
		capturedID = logging.CorrelationIDFromContext(r.Context())
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
