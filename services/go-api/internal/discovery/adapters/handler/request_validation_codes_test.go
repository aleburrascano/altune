package handler

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/httputil"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// validationRouter serves every discovery route over providers that answer, so
// a rejected request is rejected by validation and not by a missing service.
func validationRouter(t *testing.T) chi.Router {
	t.Helper()
	albumProviders := map[discdomain.ProviderName]ports.AlbumContentProvider{
		discdomain.ProviderDeezer: &fakeAlbumContentProvider{results: seedTracks(3)},
	}
	return buildDiscoveryRouter(
		&fakeSearchProvider{name: discdomain.ProviderDeezer}, &fakeSearchHistoryRepo{}, albumProviders, nil)
}

// rejectionCode is the code a caller branches on, taken off a rejected
// request. An empty one is the failure this file exists to prevent.
func rejectionCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body httputil.ErrorResponse
	discDecodeJSON(t, rec, &body)
	if body.Code == "" {
		t.Fatalf("rejected request carried no code (detail %q)", body.Detail)
	}
	return body.Code
}

// A client that cannot tell a missing q from an unrecognized kind has to match
// on the detail text, which is the failure #2233 closes.
func TestSearchRejection_MissingQIsDistinctFromUnknownKind(t *testing.T) {
	missingQ := rejectionCode(t, discServe(t, validationRouter(t), http.MethodGet, "/discovery/search", nil))
	unknownKind := rejectionCode(t, discServe(t, validationRouter(t), http.MethodGet, "/discovery/search?q=x&kinds=bogus", nil))

	if missingQ == unknownKind {
		t.Errorf("both rejections answered code %q; the causes must be distinguishable", missingQ)
	}
}

func TestSearchRejections_CodeNamesTheCause(t *testing.T) {
	cases := []struct {
		cause    string
		path     string
		wantCode string
	}{
		{"missing q", "/discovery/search", requestCodeQRequired},
		{"whitespace-only q", "/discovery/search?q=%20%20", requestCodeQRequired},
		{"unknown kind", "/discovery/search?q=x&kinds=bogus", requestCodeInvalidKind},
		{"non-numeric offset", "/discovery/search?q=x&offset=abc", requestCodeInvalidParam},
		{"non-numeric limit", "/discovery/search?q=x&limit=abc", requestCodeInvalidParam},
		{"offset past the cap", "/discovery/search?q=x&offset=100000", requestCodeInvalidParam},
		{"limit past the cap", "/discovery/search?q=x&limit=51", requestCodeInvalidParam},
		{"search_id that is not a uuid", "/discovery/search?q=x&search_id=not-a-uuid", requestCodeInvalidParam},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			rec := discServe(t, validationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != c.wantCode {
				t.Errorf("code = %q, want %q", got, c.wantCode)
			}
		})
	}
}

// A malformed paging param used to be swallowed by strconv.Atoi and served as
// page one, so a client paging with a typo got results it never asked for.
func TestSearchPaging_MalformedValueIsRejectedRatherThanDefaulted(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"non-numeric offset", "/discovery/search?q=x&offset=abc", http.StatusBadRequest},
		{"non-numeric limit", "/discovery/search?q=x&limit=abc", http.StatusBadRequest},
		{"offset overflows int", "/discovery/search?q=x&offset=99999999999999999999", http.StatusBadRequest},
		{"well-formed paging still serves", "/discovery/search?q=x&offset=10&limit=5", http.StatusOK},
		{"search_id of an expired search still serves", "/discovery/search?q=x&offset=5&search_id=" + uuid.NewString(), http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := discServe(t, validationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, c.wantStatus)
		})
	}
}

func TestSearch_AllProvidersFailedCarriesCode(t *testing.T) {
	provider := &fakeSearchProvider{name: discdomain.ProviderDeezer, err: context.DeadlineExceeded}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=x", nil)

	discAssertStatus(t, rec, http.StatusServiceUnavailable)
	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Code != searchCodeAllProvidersFailed {
		t.Errorf("code = %q, want %q", resp.Code, searchCodeAllProvidersFailed)
	}
}

func TestSearch_ServedScatterCarriesNoCode(t *testing.T) {
	rec := discServe(t, validationRouter(t), http.MethodGet, "/discovery/search?q=x", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Code != "" {
		t.Errorf("code = %q, want empty on a served search", resp.Code)
	}
}

func TestDiscoveryRejections_CodeNamesTheCauseAcrossEndpoints(t *testing.T) {
	cases := []struct {
		cause    string
		method   string
		path     string
		body     any
		wantCode string
	}{
		{
			cause: "unknown content provider", method: http.MethodGet,
			path: "/discovery/albums/bogus/id-1/tracks", wantCode: requestCodeInvalidProvider,
		},
		{
			cause: "non-numeric content limit", method: http.MethodGet,
			path: "/discovery/albums/deezer/id-1/tracks?limit=abc", wantCode: requestCodeInvalidParam,
		},
		{
			cause: "non-numeric suggest limit", method: http.MethodGet,
			path: "/discovery/suggest?q=x&limit=abc", wantCode: requestCodeInvalidParam,
		},
		{
			cause: "non-numeric search-history limit", method: http.MethodGet,
			path: "/discovery/search-history?limit=abc", wantCode: requestCodeInvalidParam,
		},
		{
			cause: "enrichment without a kind", method: http.MethodGet,
			path: "/discovery/enrichment?title=DAMN.", wantCode: requestCodeInvalidParam,
		},
		{
			cause: "enrichment with an unknown kind", method: http.MethodGet,
			path: "/discovery/enrichment?kind=bogus&title=DAMN.", wantCode: requestCodeInvalidKind,
		},
		{
			cause: "favorite with an unknown kind", method: http.MethodPut,
			path: "/discovery/favorites", body: map[string]any{"kind": "bogus", "title": "DAMN."},
			wantCode: requestCodeInvalidKind,
		},
		{
			cause: "favorite without a title", method: http.MethodPut,
			path: "/discovery/favorites", body: map[string]any{"kind": "album"},
			wantCode: requestCodeInvalidParam,
		},
		{
			cause: "event with an unknown type", method: http.MethodPost,
			path: "/discovery/events", body: map[string]any{"type": "bogus"},
			wantCode: requestCodeInvalidEventType,
		},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			var body io.Reader
			if c.body != nil {
				body = discJsonBody(t, c.body)
			}
			rec := discServe(t, validationRouter(t), c.method, c.path, body)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != c.wantCode {
				t.Errorf("code = %q, want %q", got, c.wantCode)
			}
		})
	}
}

func TestRecordEvent_UnparseableBodyCarriesCode(t *testing.T) {
	rec := discServe(t, validationRouter(t), http.MethodPost, "/discovery/events", strings.NewReader("{not json"))

	discAssertStatus(t, rec, http.StatusBadRequest)
	if got := rejectionCode(t, rec); got != requestCodeInvalidBody {
		t.Errorf("code = %q, want %q", got, requestCodeInvalidBody)
	}
}
