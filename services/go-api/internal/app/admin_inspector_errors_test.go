package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adminHandler "altune/go-api/internal/admin/handler"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// failingTransport fails every outbound provider request, simulating a total
// network-level outage for the live-provider rerun.
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: provider unreachable")
}

// inspectorAdminServer mounts the production /admin tree (bearer auth, operator
// gate) with the production inspector wiring over searchSvc and transport.
func inspectorAdminServer(t *testing.T, searchSvc *discoveryService.Service, transport http.RoundTripper) http.Handler {
	t.Helper()
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (shared.UserId, error) {
		if token == operatorToken {
			return operator, nil
		}
		return shared.UserId{}, errors.New("bad token")
	})
	artistSvc := discoveryService.NewGetArtistContentService(map[domain.ProviderName]discoveryPorts.ArtistContentProvider{})
	h := withAdminInspectors(adminHandler.New(nil, nil), &config.Config{}, transport, searchSvc, artistSvc, inspectorBudget)
	r := chi.NewRouter()
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, h)
	return r
}

type adminErrorBody struct {
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

func postInspector(t *testing.T, srv http.Handler, path, body string) (int, adminErrorBody, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var eb adminErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	return rec.Code, eb, rec.Body.String()
}

// oversizedQuery passes the handler's non-empty check but fails
// domain.NewSearchQuery's length bound.
var oversizedQuery = strings.Repeat("a", domain.MaxSearchQueryRunes+1)

// TestAdminInspectors_invalidInputIsDistinctFromProviderOutage pins #1021: a
// caller's bad input and a total provider outage must reach the operator as
// different, machine-checkable outcomes through each of /rerun, /test-search
// and /rerun-detail, instead of one untyped 502 for both.
func TestAdminInspectors_invalidInputIsDistinctFromProviderOutage(t *testing.T) {
	healthy := inspectorForProvider(outageProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{}})
	down := inspectorForProvider(outageProvider{name: domain.ProviderDeezer, err: errors.New("provider unreachable")})

	invalid := []struct {
		name, path, body string
	}{
		{"rerun invalid kinds", "/admin/rerun", `{"query":"kendrick","kinds":["bogus"]}`},
		{"rerun oversized query", "/admin/rerun", `{"query":"` + oversizedQuery + `"}`},
		{"test-search invalid kinds", "/admin/search", `{"query":"kendrick","kinds":["bogus"]}`},
		{"test-search oversized query", "/admin/search", `{"query":"` + oversizedQuery + `"}`},
		{"rerun-detail oversized query", "/admin/rerun-detail", `{"query":"` + oversizedQuery + `"}`},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			// Invalid input must be rejected before any provider is touched,
			// even when every provider is down.
			srv := inspectorAdminServer(t, down, failingTransport{})
			code, eb, raw := postInspector(t, srv, tc.path, tc.body)
			if code != http.StatusBadRequest || eb.Code != "admin.invalid_request" {
				t.Fatalf("invalid input: got %d code=%q, want 400 admin.invalid_request (body %s)", code, eb.Code, raw)
			}
			if eb.Detail == "" {
				t.Errorf("invalid input: want the validation message preserved, got empty detail")
			}
		})
	}

	t.Run("test-search outage", func(t *testing.T) {
		code, eb, raw := postInspector(t, inspectorAdminServer(t, down, failingTransport{}), "/admin/search", `{"query":"kendrick"}`)
		if code != http.StatusBadGateway || eb.Code != "admin.all_providers_failed" {
			t.Fatalf("outage: got %d code=%q, want 502 admin.all_providers_failed (body %s)", code, eb.Code, raw)
		}
	})

	t.Run("rerun-detail outage", func(t *testing.T) {
		code, eb, raw := postInspector(t, inspectorAdminServer(t, down, failingTransport{}), "/admin/rerun-detail", `{"query":"kendrick"}`)
		if code != http.StatusBadGateway || eb.Code != "admin.all_providers_failed" {
			t.Fatalf("outage: got %d code=%q, want 502 admin.all_providers_failed (body %s)", code, eb.Code, raw)
		}
	})

	// A rerun replays live providers and reports each one's trace, so a total
	// outage is a successful diagnostic whose every provider trace errored —
	// still programmatically distinct from the 400 above.
	t.Run("rerun outage", func(t *testing.T) {
		code, _, raw := postInspector(t, inspectorAdminServer(t, healthy, failingTransport{}), "/admin/rerun", `{"query":"kendrick","kinds":["artist"]}`)
		if code != http.StatusOK {
			t.Fatalf("outage: got %d, want 200 with errored provider traces (body %s)", code, raw)
		}
		var res adminHandler.ReRunResult
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			t.Fatalf("decode rerun result: %v", err)
		}
		if len(res.Providers) == 0 {
			t.Fatal("outage: want provider traces, got none")
		}
		for _, p := range res.Providers {
			if p.Status != domain.ProviderStatusError.String() {
				t.Errorf("outage: provider %s status = %q, want %q", p.Provider, p.Status, domain.ProviderStatusError.String())
			}
		}
	})

	t.Run("healthy no-match stays 200", func(t *testing.T) {
		srv := inspectorAdminServer(t, healthy, failingTransport{})
		for _, path := range []string{"/admin/search", "/admin/rerun-detail"} {
			if code, _, raw := postInspector(t, srv, path, `{"query":"kendrick"}`); code != http.StatusOK {
				t.Errorf("%s no-match: got %d, want 200 (body %s)", path, code, raw)
			}
		}
	})
}
