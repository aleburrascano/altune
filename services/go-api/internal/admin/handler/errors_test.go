package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func adminErrorBody(t *testing.T, h *AdminHandler, method, path, body string) (int, string) {
	t.Helper()
	return adminResponse(t, chi.NewRouter(), h, httptest.NewRequest(method, path, strings.NewReader(body)))
}

func adminResponse(t *testing.T, r chi.Router, h *AdminHandler, req *http.Request) (int, string) {
	t.Helper()
	h.RegisterData(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func decodedErrorBody(t *testing.T, body string) (detail, code string) {
	t.Helper()
	var resp struct {
		Detail string `json:"detail"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	return resp.Detail, resp.Code
}

func TestAdminErrorResponses_CarryStableCode(t *testing.T) {
	cases := []struct {
		name       string
		handler    *AdminHandler
		method     string
		path       string
		wantStatus int
	}{
		{"event feed unavailable", New(nil, nil), http.MethodGet, "/events/stream", http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := adminErrorBody(t, tc.handler, tc.method, tc.path, "")
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", status, tc.wantStatus, body)
			}
			if _, code := decodedErrorBody(t, body); code == "" {
				t.Errorf("error response has empty code field: %s", body)
			}
		})
	}
}

func TestAdminRequiredParam400s_CarryStableCode(t *testing.T) {
	cases := []struct {
		name     string
		handler  *AdminHandler
		method   string
		path     string
		body     string
		wantCode string
	}{
		{"metrics missing metric", New(nil, nil), http.MethodGet, "/metrics", "", "admin.metric_required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := adminErrorBody(t, tc.handler, tc.method, tc.path, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %s)", status, http.StatusBadRequest, body)
			}
			detail, code := decodedErrorBody(t, body)
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q (body %s)", code, tc.wantCode, body)
			}
			if detail == "" {
				t.Errorf("error response has empty detail: %s", body)
			}
		})
	}
}
