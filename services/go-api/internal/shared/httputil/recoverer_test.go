package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
