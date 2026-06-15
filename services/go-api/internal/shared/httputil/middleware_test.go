package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCorrelationID_SetsHeader(t *testing.T) {
	// Arrange
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	corrID := rec.Header().Get("X-Correlation-ID")
	if corrID == "" {
		t.Fatal("expected X-Correlation-ID header to be set, got empty")
	}
	if len(corrID) != 8 {
		t.Errorf("X-Correlation-ID length = %d, want 8 (uuid[:8])", len(corrID))
	}
}

func TestCorrelationID_PropagatesInContext(t *testing.T) {
	// Arrange
	var capturedID string
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetCorrelationID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if capturedID == "" {
		t.Fatal("expected correlation ID in context, got empty")
	}
	headerID := rec.Header().Get("X-Correlation-ID")
	if capturedID != headerID {
		t.Errorf("context ID %q does not match header ID %q", capturedID, headerID)
	}
}

func TestCorrelationID_UniqueBetweenRequests(t *testing.T) {
	// Arrange
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	rec2 := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/a", nil))
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/b", nil))

	// Assert
	id1 := rec1.Header().Get("X-Correlation-ID")
	id2 := rec2.Header().Get("X-Correlation-ID")
	if id1 == id2 {
		t.Errorf("expected unique correlation IDs between requests, both are %q", id1)
	}
}

func TestGetCorrelationID_EmptyContext(t *testing.T) {
	// Arrange: context without correlation ID
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// Act
	id := GetCorrelationID(req.Context())

	// Assert
	if id != "" {
		t.Errorf("expected empty string for context without correlation ID, got %q", id)
	}
}

func TestRequestLogger_DoesNotPanic(t *testing.T) {
	// Arrange
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act + Assert: no panic
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequestLogger_TracksStatusCode(t *testing.T) {
	// Arrange: handler returns 404
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRecoverer_CatchesPanic_Returns500(t *testing.T) {
	// Arrange: handler panics
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went very wrong")
	}))
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
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
	// Arrange: handler does not panic
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fine"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "fine" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fine")
	}
}

func TestStatusWriter_DefaultStatus200(t *testing.T) {
	// Arrange: statusWriter wrapping a recorder, no explicit WriteHeader call
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	// Act: write body without calling WriteHeader
	n, err := sw.Write([]byte("hello"))

	// Assert
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
	// Arrange
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	// Act
	sw.WriteHeader(http.StatusCreated)
	sw.Write([]byte("abc"))
	sw.Write([]byte("de"))

	// Assert
	if sw.status != http.StatusCreated {
		t.Errorf("sw.status = %d, want %d", sw.status, http.StatusCreated)
	}
	if sw.bytes != 5 {
		t.Errorf("sw.bytes = %d, want 5 (3+2)", sw.bytes)
	}
}
