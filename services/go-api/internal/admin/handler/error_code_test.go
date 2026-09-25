package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func erroringReRun(context.Context, string, []string) (requeststore.ReRunResult, error) {
	return requeststore.ReRunResult{}, errors.New("upstream exploded")
}

// failingReRun answers every rerun with failure, standing in for whatever the
// inspector wiring reports upward.
func failingReRun(failure error) ReRunner {
	return func(context.Context, string, []string) (requeststore.ReRunResult, error) {
		return requeststore.ReRunResult{}, failure
	}
}

func erroringInspect(context.Context, string, []string) ([]requeststore.ResultRow, error) {
	return nil, errors.New("upstream exploded")
}

func erroringDetail(context.Context, string) (requeststore.DetailReRunResult, error) {
	return requeststore.DetailReRunResult{}, errors.New("upstream exploded")
}

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
			if _, code := decodedErrorBody(t, body); code == "" {
				t.Errorf("error response has empty code field: %s", body)
			}
		})
	}
}

// TestAdminRequiredParam400s_CarryStableCode pins the required-param rejections
// (#1003): the 400s share their endpoints with coded 502/503 branches, so they
// must carry a branchable code too, with the status unchanged.
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
		{"rerun empty query", New(nil, nil).WithReRunner(erroringReRun), http.MethodPost, "/rerun", `{"query":""}`, "admin.query_required"},
		{"rerun truncated body", New(nil, nil).WithReRunner(erroringReRun), http.MethodPost, "/rerun", `{`, "admin.invalid_json"},
		{"rerun malformed body", New(nil, nil).WithReRunner(erroringReRun), http.MethodPost, "/rerun", `{bad`, "admin.invalid_json"},
		{"rerun non-object body", New(nil, nil).WithReRunner(erroringReRun), http.MethodPost, "/rerun", `"query"`, "admin.invalid_json"},
		{"search empty query", New(nil, nil).WithSearchInspector(erroringInspect), http.MethodPost, "/search", `{}`, "admin.query_required"},
		{"rerun-detail empty query", New(nil, nil).WithDetailReRunner(erroringDetail), http.MethodPost, "/rerun-detail", "", "admin.query_required"},
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

// TestAdminInspectorAbort_isDistinctFromUpstreamFailure reproduces #2006: a
// deadline hit and a client disconnect answered the same 502 as a broken
// provider, so a caller could not tell a retryable timeout from a bug.
func TestAdminInspectorAbort_isDistinctFromUpstreamFailure(t *testing.T) {
	expired, releaseExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
	defer releaseExpired()
	disconnected, disconnect := context.WithCancel(context.Background())
	disconnect()

	cases := []struct {
		name       string
		ctx        context.Context
		failure    error
		wantStatus int
		wantCode   string
	}{
		{"inspector reports the deadline", context.Background(), fmt.Errorf("deezer: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, "admin.timeout"},
		{"inspector reports the cancel", context.Background(), fmt.Errorf("deezer: %w", context.Canceled), 499, "admin.client_closed_request"},
		{"providers fail under an expired request", expired, errors.New("every provider failed"), http.StatusGatewayTimeout, "admin.timeout"},
		{"providers fail under a disconnected client", disconnected, errors.New("every provider failed"), 499, "admin.client_closed_request"},
		{"providers fail under a live request", context.Background(), errors.New("upstream exploded"), http.StatusBadGateway, "admin.rerun_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/rerun", strings.NewReader(`{"query":"x"}`)).WithContext(tc.ctx)
			status, body := adminResponse(t, chi.NewRouter(), New(nil, nil).WithReRunner(failingReRun(tc.failure)), req)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", status, tc.wantStatus, body)
			}
			if _, code := decodedErrorBody(t, body); code != tc.wantCode {
				t.Errorf("code = %q, want %q (body %s)", code, tc.wantCode, body)
			}
		})
	}
}

// TestAdminInspectorFailure_bodyHidesUpstreamDetail reproduces #2006: an
// unclassified inspector failure is typically a transport error carrying the
// provider URL and its credentials, which belong in the log, not in a response.
func TestAdminInspectorFailure_bodyHidesUpstreamDetail(t *testing.T) {
	leak := fmt.Errorf(`rerun: Get %q: dial tcp 10.1.2.3:443: connection refused`,
		"https://api.provider.test/search?q=x&api_key=SUPERSECRET")

	status, body := adminErrorBody(t, New(nil, nil).WithReRunner(failingReRun(leak)), http.MethodPost, "/rerun", `{"query":"x"}`)

	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d (body %s)", status, http.StatusBadGateway, body)
	}
	for _, leaked := range []string{"SUPERSECRET", "api_key", "api.provider.test", "10.1.2.3"} {
		if strings.Contains(body, leaked) {
			t.Errorf("response body leaks upstream detail %q: %s", leaked, body)
		}
	}
	detail, code := decodedErrorBody(t, body)
	if detail == "" || code != "admin.rerun_failed" {
		t.Errorf("want a generic detail under the endpoint's code, got detail=%q code=%q", detail, code)
	}
}

// TestAdminOversizeBody_isRejectedAs413 reproduces #2006: a body past the
// server's ceiling reached the operator as "query is required", which points at
// the wrong fix.
func TestAdminOversizeBody_isRejectedAs413(t *testing.T) {
	const bodyLimit = 32
	oversize := `{"query":"` + strings.Repeat("a", 4*bodyLimit) + `"}`

	r := chi.NewRouter()
	r.Use(httputil.MaxBodySize(bodyLimit))
	req := httptest.NewRequest(http.MethodPost, "/rerun", strings.NewReader(oversize))
	status, body := adminResponse(t, r, New(nil, nil).WithReRunner(erroringReRun), req)

	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (body %s)", status, http.StatusRequestEntityTooLarge, body)
	}
	if _, code := decodedErrorBody(t, body); code != "admin.body_too_large" {
		t.Errorf("code = %q, want %q (body %s)", code, "admin.body_too_large", body)
	}
}
