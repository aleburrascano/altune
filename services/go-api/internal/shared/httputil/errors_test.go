package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, payload{Name: "altune"})

	// Status code
	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusCreated)
	}

	// Content-Type
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	// Body
	var got payload
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Name != "altune" {
		t.Errorf("body.name: got %q, want %q", got.Name, "altune")
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusTeapot, "I'm a teapot")

	if rec.Code != http.StatusTeapot {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusTeapot)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "I'm a teapot" {
		t.Errorf("detail: got %q, want %q", body.Detail, "I'm a teapot")
	}
}

func TestNotFound(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantDetail string
	}{
		{
			name:       "custom message",
			message:    "track not found",
			wantDetail: "track not found",
		},
		{
			name:       "empty message uses default",
			message:    "",
			wantDetail: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NotFound(rec, tt.message)

			if rec.Code != http.StatusNotFound {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusNotFound)
			}

			var body ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Detail != tt.wantDetail {
				t.Errorf("detail: got %q, want %q", body.Detail, tt.wantDetail)
			}
		})
	}
}

func TestUnauthorized(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantDetail string
	}{
		{
			name:       "custom message",
			message:    "token expired",
			wantDetail: "token expired",
		},
		{
			name:       "empty message uses default",
			message:    "",
			wantDetail: "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Unauthorized(rec, tt.message)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
			}

			var body ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Detail != tt.wantDetail {
				t.Errorf("detail: got %q, want %q", body.Detail, tt.wantDetail)
			}
		})
	}
}

func TestBadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	BadRequest(rec, "invalid input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "invalid input" {
		t.Errorf("detail: got %q, want %q", body.Detail, "invalid input")
	}
}

func TestInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	InternalError(rec)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "internal server error" {
		t.Errorf("detail: got %q, want %q", body.Detail, "internal server error")
	}
}

func TestConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	Conflict(rec, "already exists")

	if rec.Code != http.StatusConflict {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusConflict)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "already exists" {
		t.Errorf("detail: got %q, want %q", body.Detail, "already exists")
	}
}
