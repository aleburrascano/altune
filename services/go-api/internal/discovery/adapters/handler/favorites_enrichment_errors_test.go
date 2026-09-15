package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// statusCodedError carries its own HTTP status and machine-readable code
// through the httputil StatusError/ErrorCoder contract.
type statusCodedError struct {
	status int
	code   string
}

func (e statusCodedError) Error() string     { return "classified failure" }
func (e statusCodedError) HTTPStatus() int   { return e.status }
func (e statusCodedError) ErrorCode() string { return e.code }

type erroringFavoritesRepo struct{ err error }

func (r erroringFavoritesRepo) Add(context.Context, shared.UserId, discdomain.Favorite) error {
	return r.err
}

func (r erroringFavoritesRepo) Remove(context.Context, shared.UserId, discdomain.ResultKind, string) error {
	return r.err
}

func (r erroringFavoritesRepo) ListForUser(context.Context, shared.UserId) ([]discdomain.Favorite, error) {
	return nil, r.err
}

func buildFavoritesRouter(err error) chi.Router {
	svc := service.NewFavoritesService(erroringFavoritesRepo{err: err})
	h := NewDiscoveryHandler(DiscoveryServices{Favorites: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	discAssertStatus(t, rec, wantStatus)
	discAssertJSON(t, rec)
	var resp httputil.ErrorResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Code != wantCode {
		t.Errorf("code = %q, want %q (detail: %q)", resp.Code, wantCode, resp.Detail)
	}
}

func TestFavoritesEndpoints_ServiceErrorsUseTypedContract(t *testing.T) {
	const favBody = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`
	requests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "list", method: http.MethodGet},
		{name: "add", method: http.MethodPut, body: favBody},
		{name: "remove", method: http.MethodDelete, body: favBody},
	}
	errs := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "validation",
			err:        shared.NewValidationError("discovery", "favorite is invalid"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "discovery.validation_error",
		},
		{
			name:       "not found",
			err:        statusCodedError{status: http.StatusNotFound},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "transient",
			err:        statusCodedError{status: http.StatusServiceUnavailable, code: "favorites_unavailable"},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "favorites_unavailable",
		},
		{
			name:       "unclassified",
			err:        errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
	}

	for _, req := range requests {
		for _, e := range errs {
			t.Run(req.name+" "+e.name, func(t *testing.T) {
				router := buildFavoritesRouter(e.err)
				var body io.Reader
				if req.body != "" {
					body = strings.NewReader(req.body)
				}
				rec := discServe(t, router, req.method, "/discovery/favorites", body)
				assertErrorCode(t, rec, e.wantStatus, e.wantCode)
			})
		}
	}
}

// No enrichment service currently returns a non-degraded error, so the hard
// error path of the shared withEnricher helper is pinned directly.
func TestWithEnricher_HardErrorsUseTypedContract(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "validation",
			err:        shared.NewValidationError("discovery", "bad enrichment request"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "discovery.validation_error",
		},
		{
			name:       "transient",
			err:        statusCodedError{status: http.StatusServiceUnavailable, code: "enrichment_unavailable"},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "enrichment_unavailable",
		},
		{
			name:       "unclassified",
			err:        errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/discovery/enrichment", nil)
			rec := httptest.NewRecorder()
			withEnricher(rec, req, true,
				func() any { return struct{}{} },
				func() (any, error) { return nil, tc.err },
				"enrichment failed")
			assertErrorCode(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

// A typed upstream status (e.g. a provider 404 or 503) must stay a degraded
// 200 through the real enrichment handlers, never leak as the endpoint status.
func TestEnrichmentEndpoints_TypedProviderErrorStaysDegraded(t *testing.T) {
	upstream := statusCodedError{status: http.StatusNotFound, code: "upstream"}
	svc := enrich.NewLastFmEnrichmentService(&scriptedLastFmEnricher{err: upstream},
		newMemNameCache[discdomain.LastFmEnrichment]())
	router := buildEnrichersRouter(DetailEnrichers{LastFm: svc})

	rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?kind=artist&title=Nas", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var resp LastFmEnrichmentResponseDTO
	discDecodeJSON(t, rec, &resp)
	if !resp.Degraded {
		t.Errorf("degraded = false, want true")
	}
}
