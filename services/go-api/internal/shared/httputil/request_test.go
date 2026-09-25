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

func decodeThroughCap(body string, limit int64) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var dst map[string]any
	h := MaxBodySize(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		DecodeJSON(w, r, &dst)
	}))
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	return rec
}

func TestDecodeJSONLimits(t *testing.T) {
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"oversize body", `{"a":"` + strings.Repeat("x", 64) + `"}`, "request.too_large", http.StatusRequestEntityTooLarge},
		{"trailing data", `{"a":1}garbage`, "request.invalid_body", http.StatusBadRequest},
		{"malformed", `{bad`, "request.invalid_body", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := decodeThroughCap(tc.body, 16)
			if rec.Code != tc.status {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.status)
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("body: got %s, want code %s", rec.Body.String(), tc.code)
			}
		})
	}
}

func TestDecodeJSONRejectsStrayClosingByte(t *testing.T) {
	for _, body := range []string{`{"a":1}}`, `{"a":1}]`, `{"a":1} {"a":2}`} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		rec := httptest.NewRecorder()
		var dst struct{ A int }
		if DecodeJSON(rec, req, &dst) || rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: want rejected with 400, got code %d", body, rec.Code)
		}
	}
}
