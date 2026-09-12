package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func erroringReRun(context.Context, string, []string) (requeststore.ReRunResult, error) {
	return requeststore.ReRunResult{}, errors.New("upstream exploded")
}

func erroringInspect(context.Context, string, []string) ([]requeststore.ResultRow, error) {
	return nil, errors.New("upstream exploded")
}

func erroringDetail(context.Context, string) (requeststore.DetailReRunResult, error) {
	return requeststore.DetailReRunResult{}, errors.New("upstream exploded")
}

func adminErrorBody(t *testing.T, h *AdminHandler, method, path, body string) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterData(r)

	var reqBody *strings.Reader
	if body == "" {
		reqBody = strings.NewReader("")
	} else {
		reqBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestAdminErrorResponses_CarryStableCode(t *testing.T) {
	validQuery := `{"query":"x"}`

	cases := []struct {
		name       string
		handler    *AdminHandler
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"rerun unavailable", New(nil, nil), http.MethodPost, "/rerun", validQuery, http.StatusServiceUnavailable},
		{"rerun upstream", New(nil, nil).WithReRunner(erroringReRun), http.MethodPost, "/rerun", validQuery, http.StatusBadGateway},
		{"search unavailable", New(nil, nil), http.MethodPost, "/search", validQuery, http.StatusServiceUnavailable},
		{"search upstream", New(nil, nil).WithSearchInspector(erroringInspect), http.MethodPost, "/search", validQuery, http.StatusBadGateway},
		{"rerun-detail unavailable", New(nil, nil), http.MethodPost, "/rerun-detail", validQuery, http.StatusServiceUnavailable},
		{"rerun-detail upstream", New(nil, nil).WithDetailReRunner(erroringDetail), http.MethodPost, "/rerun-detail", validQuery, http.StatusBadGateway},
		{"request not found", New(nil, nil), http.MethodGet, "/requests/missing", "", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := adminErrorBody(t, tc.handler, tc.method, tc.path, tc.body)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", status, tc.wantStatus, body)
			}
			var resp struct {
				Detail string `json:"detail"`
				Code   string `json:"code"`
			}
			if err := json.Unmarshal([]byte(body), &resp); err != nil {
				t.Fatalf("decode body %q: %v", body, err)
			}
			if resp.Code == "" {
				t.Errorf("error response has empty code field: %s", body)
			}
		})
	}
}
