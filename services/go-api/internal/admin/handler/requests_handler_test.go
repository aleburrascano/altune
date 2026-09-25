package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The fixture trace carries the two values the console must not hand out on a
// list: the end user's Supabase id and a captured provider body. Both are
// searched for verbatim in the served JSON, so the marker is plain ASCII that
// JSON escaping cannot disguise.
const (
	tracedCorrID     = "c-trace-1"
	tracedUserID     = "9f1c3a42-7b60-4d5e-8a11-2c3d4e5f6a7b"
	tracedUserDigest = "52cf2ad1"
	tracedQuery      = "ken carson"
	tracedBodyMarker = "PROVIDER-PAYLOAD-MARKER"
)

// stubProviderBody answers every round trip with body, standing in for the
// provider the recording transport is wrapped around.
type stubProviderBody string

func (b stubProviderBody) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
}

// tracedStore holds one record with every field a record can carry — search
// trace, captured exchange, content-fetch trace — so a projection that forwards
// a field it should not is visible in the response.
func tracedStore(t *testing.T) *requeststore.Store {
	t.Helper()
	store := requeststore.New()
	ctx := logging.WithCorrelationID(t.Context(), tracedCorrID)
	captureProviderExchange(ctx, t, store)
	store.RecordSearch(ctx, tracedQuery, []string{"artist"}, tracedUserID, tracedStatuses(), tracedResults())
	fetch := ports.ContentFetchEvent{Kind: "albums", Provider: "deezer", Artist: "Ken Carson", Status: "ok"}
	store.RecordContentFetch(ctx, fetch, tracedResults())
	return store
}

func tracedStatuses() []domain.ProviderSearchResponse {
	return []domain.ProviderSearchResponse{{
		Provider:    domain.ProviderDeezer,
		Status:      domain.ProviderStatusOK,
		LatencyMs:   12,
		ResultCount: 1,
		Results:     tracedResults(),
	}}
}

func tracedResults() []domain.SearchResult {
	return []domain.SearchResult{{Kind: domain.ResultKindArtist, Title: "Ken Carson", Year: 2023}}
}

// captureProviderExchange drives one round trip through the store's recording
// transport, the only exported path that puts a response body on a record.
func captureProviderExchange(ctx context.Context, t *testing.T, store *requeststore.Store) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deezer.com/search", nil)
	if err != nil {
		t.Fatalf("build provider request: %v", err)
	}
	body := `{"data":[{"title":"` + tracedBodyMarker + `"}]}`
	resp, err := requeststore.NewCorrelatedTransport(stubProviderBody(body), store).RoundTrip(req)
	if err != nil {
		t.Fatalf("provider round trip: %v", err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("drain provider body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close provider body: %v", err)
	}
}

func requestsResponse(t *testing.T, path string) string {
	t.Helper()
	h := New(nil, nil).WithRequestStore(tracedStore(t))
	code, body := adminResponse(t, chi.NewRouter(), h, httptest.NewRequest(http.MethodGet, path, nil))
	if code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (body %s)", path, code, body)
	}
	return body
}

// TestServeRequests_ListIsASummaryWithoutUserOrBodies is the regression guard
// for #1997: GET /admin/requests served the stored records whole, so every
// read-only operator got the end user's Supabase id and every raw provider body
// for the retention window. The key set is asserted exactly, so a field added to
// the record later cannot leak here by default.
func TestServeRequests_ListIsASummaryWithoutUserOrBodies(t *testing.T) {
	body := requestsResponse(t, "/requests")

	var list []map[string]any
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode list %q: %v", body, err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1 (body %s)", len(list), body)
	}

	want := []string{
		"corr_id", "detail", "exchange_count", "final_count",
		"kinds", "provider_count", "query", "started_at",
	}
	if got := slices.Sorted(maps.Keys(list[0])); !slices.Equal(got, want) {
		t.Errorf("list keys = %v, want %v", got, want)
	}
	if strings.Contains(body, tracedUserID) {
		t.Errorf("list served the end user's id: %s", body)
	}
	if strings.Contains(body, tracedBodyMarker) {
		t.Errorf("list served a raw provider body: %s", body)
	}
}

func TestServeRequests_ListCountsMatchTheRecord(t *testing.T) {
	body := requestsResponse(t, "/requests")

	var list []struct {
		ExchangeCount int `json:"exchange_count"`
		ProviderCount int `json:"provider_count"`
		FinalCount    int `json:"final_count"`
		Detail        struct {
			ItemCount int `json:"item_count"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode list %q: %v", body, err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1 (body %s)", len(list), body)
	}
	got := list[0]
	if got.ExchangeCount != 1 || got.ProviderCount != 1 || got.FinalCount != 1 || got.Detail.ItemCount != 1 {
		t.Errorf("counts = %+v, want one of each", got)
	}
}

// TestServeRequestDetail_PseudonymisesTheUser pins the other half of #1997: the
// drill-down still carries the bodies an operator debugs with, but the caller is
// identified only by a digest, so two records can be tied to one caller without
// the console ever holding the Supabase id.
func TestServeRequestDetail_PseudonymisesTheUser(t *testing.T) {
	body := requestsResponse(t, "/requests/"+tracedCorrID)

	var detail struct {
		User      string `json:"user"`
		Exchanges []struct {
			RespBody string `json:"response_body"`
		} `json:"exchanges"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("decode detail %q: %v", body, err)
	}
	if strings.Contains(body, tracedUserID) {
		t.Errorf("detail served the end user's id: %s", body)
	}
	if detail.User != tracedUserDigest {
		t.Errorf("detail user = %q, want the digest %q", detail.User, tracedUserDigest)
	}
	if len(detail.Exchanges) != 1 || !strings.Contains(detail.Exchanges[0].RespBody, tracedBodyMarker) {
		t.Errorf("detail must keep the captured provider body: %s", body)
	}
}

func TestServeRequestDetail_UnknownCorrIDIsNotFound(t *testing.T) {
	h := New(nil, nil).WithRequestStore(tracedStore(t))
	req := httptest.NewRequest(http.MethodGet, "/requests/c-never-recorded", nil)

	code, body := adminResponse(t, chi.NewRouter(), h, req)
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", code, body)
	}
}

func TestServeRequests_NoStoreServesEmptyList(t *testing.T) {
	code, body := adminResponse(t, chi.NewRouter(), New(nil, nil), httptest.NewRequest(http.MethodGet, "/requests", nil))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", code, body)
	}
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}
