package handler

import (
	"altune/go-api/internal/shared/httputil"
	"errors"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// classifiedTestError is a service failure that carries its own HTTP status
// and machine-readable code through the httputil StatusError/ErrorCoder
// contract.
type classifiedTestError struct{}

func (classifiedTestError) Error() string     { return "history store unavailable" }
func (classifiedTestError) HTTPStatus() int   { return http.StatusServiceUnavailable }
func (classifiedTestError) ErrorCode() string { return "history_unavailable" }

func TestSearchEndpoints_ServiceErrorsUseTypedContract(t *testing.T) {
	classified := classifiedTestError{}
	unclassified := errors.New("boom")

	cases := []struct {
		name       string
		router     func(err error) chi.Router
		method     string
		path       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "suggest classified failure",
			router:     func(err error) chi.Router { return buildSuggestRouter(&fakeVocabStore{err: err}) },
			method:     http.MethodGet,
			path:       "/discovery/suggest?q=kend",
			err:        classified,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "history_unavailable",
		},
		{
			name:       "suggest unclassified failure",
			router:     func(err error) chi.Router { return buildSuggestRouter(&fakeVocabStore{err: err}) },
			method:     http.MethodGet,
			path:       "/discovery/suggest?q=kend",
			err:        unclassified,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
		{
			name: "search history classified failure",
			router: func(err error) chi.Router {
				return buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{err: err}, nil, nil)
			},
			method:     http.MethodGet,
			path:       "/discovery/search-history?limit=10",
			err:        classified,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "history_unavailable",
		},
		{
			name: "clear search history classified failure",
			router: func(err error) chi.Router {
				return buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{err: err}, nil, nil)
			},
			method:     http.MethodDelete,
			path:       "/discovery/search-history",
			err:        classified,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "history_unavailable",
		},
		{
			name: "clear search history unclassified failure",
			router: func(err error) chi.Router {
				return buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{err: err}, nil, nil)
			},
			method:     http.MethodDelete,
			path:       "/discovery/search-history",
			err:        unclassified,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := tc.router(tc.err)
			rec := discServe(t, router, tc.method, tc.path, nil)

			discAssertStatus(t, rec, tc.wantStatus)
			discAssertJSON(t, rec)
			var resp httputil.ErrorResponse
			discDecodeJSON(t, rec, &resp)
			if resp.Code != tc.wantCode {
				t.Errorf("code = %q, want %q (body detail: %q)", resp.Code, tc.wantCode, resp.Detail)
			}
		})
	}
}
