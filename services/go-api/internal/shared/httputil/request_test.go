package httputil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDecodeJSON(t *testing.T) {
	t.Run("valid body decodes and returns true", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"hi"}`))

		var body struct {
			Name string `json:"name"`
		}
		if ok := DecodeJSON(rec, req, &body); !ok {
			t.Fatal("DecodeJSON: got false, want true")
		}
		if body.Name != "hi" {
			t.Errorf("decoded name: got %q, want %q", body.Name, "hi")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("untouched recorder status: got %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("invalid body writes byte-identical 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{bad`))

		var body struct{ Name string }
		if ok := DecodeJSON(rec, req, &body); ok {
			t.Fatal("DecodeJSON: got true, want false")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"detail":"invalid request body","code":"request.invalid_body"}` {
			t.Errorf("body: got %s, want %s", got, `{"detail":"invalid request body","code":"request.invalid_body"}`)
		}
	})
}

func newRequestWithURLParam(name, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(name, value)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestPathID(t *testing.T) {
	t.Run("valid param parses and returns true", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := newRequestWithURLParam("id", "42")

		id, ok := PathID(rec, req, "id", strconv.Atoi, "invalid id")
		if !ok {
			t.Fatal("PathID: got false, want true")
		}
		if id != 42 {
			t.Errorf("parsed id: got %d, want %d", id, 42)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("untouched recorder status: got %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("invalid param writes byte-identical 400 with the supplied message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := newRequestWithURLParam("id", "not-a-number")

		id, ok := PathID(rec, req, "id", strconv.Atoi, "invalid track ID")
		if ok {
			t.Fatal("PathID: got true, want false")
		}
		if id != 0 {
			t.Errorf("zero value: got %d, want 0", id)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"detail":"invalid track ID"}` {
			t.Errorf("body: got %s, want %s", got, `{"detail":"invalid track ID"}`)
		}
	})
}
