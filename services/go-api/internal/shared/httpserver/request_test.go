package httpserver_test

import (
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
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
		if ok := httpserver.DecodeJSON(rec, req, &body); !ok {
			t.Fatal("DecodeJSON: got false, want true")
		}
		if body.Name != "hi" {
			t.Errorf("decoded name: got %q, want %q", body.Name, "hi")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("untouched recorder status: got %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("invalid body answers a 400 problem", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{bad`))

		var body struct{ Name string }
		if ok := httpserver.DecodeJSON(rec, req, &body); ok {
			t.Fatal("DecodeJSON: got true, want false")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if p := decodeProblem(t, rec); p.Detail != "invalid request body" || p.Code != "request.invalid_body" || p.Status != http.StatusBadRequest {
			t.Errorf("problem = %+v, want 400 request.invalid_body", p)
		}
	})
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"hi","extra":1}`))

	var body struct {
		Name string `json:"name"`
	}
	if ok := httpserver.DecodeJSON(rec, req, &body); ok {
		t.Fatal("DecodeJSON: got true, want false for an unknown field")
	}
	if p := decodeProblem(t, rec); p.Code != "request.invalid_body" || p.Status != http.StatusBadRequest {
		t.Errorf("problem = %+v, want 400 request.invalid_body", p)
	}
}

func TestQueryInt(t *testing.T) {
	code := apierror.CodeDiscoveryInvalidParam
	cases := []struct {
		name   string
		query  string
		want   int
		detail string
	}{
		{name: "absent uses the default", query: "", want: 7},
		{name: "in range", query: "limit=3", want: 3},
		{name: "lower bound", query: "limit=1", want: 1},
		{name: "upper bound", query: "limit=10", want: 10},
		{name: "not an integer", query: "limit=abc", detail: "limit must be an integer"},
		{name: "fraction", query: "limit=1.5", detail: "limit must be an integer"},
		{name: "overflow", query: "limit=99999999999999999999", detail: "limit must be an integer"},
		{name: "below range", query: "limit=0", detail: "limit must be between 1 and 10"},
		{name: "above range", query: "limit=11", detail: "limit must be between 1 and 10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/x?"+tc.query, nil)

			got, ok := httpserver.QueryInt(rec, req, "limit", 7, 1, 10, code)

			if tc.detail == "" {
				if !ok || got != tc.want {
					t.Fatalf("QueryInt = %d, %v; want %d, true", got, ok, tc.want)
				}
				return
			}
			if ok {
				t.Fatalf("QueryInt = %d, true; want a rejection", got)
			}
			if p := decodeProblem(t, rec); p.Detail != tc.detail || p.Code != code.String() || p.Status != http.StatusBadRequest {
				t.Errorf("problem = %+v, want 400 %s %q", p, code, tc.detail)
			}
		})
	}
}

func TestURLParam(t *testing.T) {
	if got := httpserver.URLParam(newRequestWithURLParam("id", "abc"), "id"); got != "abc" {
		t.Errorf("URLParam = %q, want abc", got)
	}
}

func TestIdempotencyKey(t *testing.T) {
	tests := []struct {
		name   string
		header *string
		want   *string
	}{
		{name: "absent header", header: nil, want: nil},
		{name: "blank header", header: ptr("   "), want: nil},
		{name: "trimmed key", header: ptr("  key-1 "), want: ptr("key-1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.header != nil {
				req.Header.Set(httpserver.IdempotencyKeyHeader, *tt.header)
			}

			got := httpserver.IdempotencyKey(req)

			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("IdempotencyKey = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }

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

		id, ok := httpserver.PathID(rec, req, "id", strconv.Atoi, "invalid id")
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

	t.Run("invalid param writes a coded 400 with the supplied message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := newRequestWithURLParam("id", "not-a-number")

		id, ok := httpserver.PathID(rec, req, "id", strconv.Atoi, "invalid track ID")
		if ok {
			t.Fatal("PathID: got true, want false")
		}
		if id != 0 {
			t.Errorf("zero value: got %d, want 0", id)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if p := decodeProblem(t, rec); p.Detail != "invalid track ID" || p.Code != "request.invalid_id" || p.Status != http.StatusBadRequest {
			t.Errorf("problem = %+v, want 400 request.invalid_id", p)
		}
	})
}

func decodeThroughCap(body string, limit int64) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var dst map[string]any
	h := httpserver.MaxBodySize(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpserver.DecodeJSON(w, r, &dst)
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
		if httpserver.DecodeJSON(rec, req, &dst) || rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: want rejected with 400, got code %d", body, rec.Code)
		}
	}
}
