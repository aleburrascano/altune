package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// providerUsageBody is a representative GET /observe/metrics/live payload for the
// provider-usage read: the per-provider outbound-call counts the Cost bucket
// reads, plus the latency and per-module fields it ignores. It proves the mirror
// decodes only the "providers" block while tolerating the sibling fields it does
// not model.
const providerUsageBody = `{
	"auth": {"token_rejections_total": 3},
	"latency": {"routes": {}},
	"providers": {
		"deezer":  {"ok": 120, "quota": 4, "error": 2},
		"spotify": {"ok": 30, "quota": 0, "error": 1},
		"other":   {"ok": 0, "quota": 0, "error": 0}
	}
}`

// TestAdminProviderUsageDecodesStubbedResponse is the core Done proof for the
// read: the client hits a stubbed go-api /observe/metrics/live and decodes the
// per-provider call counts, ignoring the latency and counter fields it does not
// model.
func TestAdminProviderUsageDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/metrics/live" {
			t.Errorf("stub got path %s, want /observe/metrics/live", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(providerUsageBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminProviderUsage(context.Background())
	if err != nil {
		t.Fatalf("AdminProviderUsage: unexpected error: %v", err)
	}
	deezer, ok := got["deezer"]
	if !ok {
		t.Fatalf("deezer not decoded; got providers %+v", got)
	}
	if deezer.OK != 120 || deezer.Quota != 4 || deezer.Error != 2 {
		t.Fatalf("deezer = %+v, want ok=120 quota=4 error=2", deezer)
	}
	if got := deezer.Total(); got != 126 {
		t.Fatalf("deezer.Total() = %d, want 126", got)
	}
	if spotify := got["spotify"]; spotify.OK != 30 || spotify.Error != 1 {
		t.Fatalf("spotify = %+v, want ok=30 error=1", spotify)
	}
	if other := got["other"]; other.Total() != 0 {
		t.Fatalf("other.Total() = %d, want 0", other.Total())
	}
}

// TestAdminProviderUsageAttachesOperatorBearer proves the operator principal is
// authenticated on the operator-guarded read: without the bearer, go-api's
// OperatorOnly guard would 403 the provider counts.
func TestAdminProviderUsageAttachesOperatorBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"providers":{}}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminProviderUsage(context.Background()); err != nil {
		t.Fatalf("AdminProviderUsage: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

// TestAdminProviderUsageUnreachableYieldsSourceDown proves an unreachable go-api
// surfaces as the typed source-down error the bucket branches on to render the
// provider half stale.
func TestAdminProviderUsageUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := newClient(t, url).AdminProviderUsage(context.Background())
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

// TestAdminProviderUsageForbiddenYieldsAPIError proves the auth-rejection path: a
// non-operator principal is a reachable-but-refused read, so it surfaces as an
// APIError (go-api is up; it said no), NOT source-down.
func TestAdminProviderUsageForbiddenYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).AdminProviderUsage(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("403 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("APIError.StatusCode = %d, want 403", apiErr.StatusCode)
	}
}

// TestProviderOutcomesTotalSaturatesOnOverflow proves a hostile or corrupt go-api
// response with per-outcome counts near the int64 ceiling cannot wrap the
// provider total: a plain int64 add would wrap MaxInt64+MaxInt64 to -2 (a huge
// active provider misread as inactive, desyncing the render gates). The
// saturating sum pins the total at MaxInt64 instead, staying large-and-positive,
// and is zero only when every outcome is genuinely zero.
func TestProviderOutcomesTotalSaturatesOnOverflow(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    goapi.ProviderOutcomes
		want int64
	}{
		{"benign sum", goapi.ProviderOutcomes{OK: 120, Quota: 4, Error: 2}, 126},
		{"all zero stays zero", goapi.ProviderOutcomes{}, 0},
		{"two at ceiling saturate", goapi.ProviderOutcomes{OK: math.MaxInt64, Quota: math.MaxInt64}, math.MaxInt64},
		{"all three at ceiling saturate", goapi.ProviderOutcomes{OK: math.MaxInt64, Quota: math.MaxInt64, Error: math.MaxInt64}, math.MaxInt64},
		{"ceiling plus one saturates", goapi.ProviderOutcomes{OK: math.MaxInt64, Error: 1}, math.MaxInt64},
	} {
		if got := tc.o.Total(); got != tc.want {
			t.Errorf("%s: Total() = %d, want %d", tc.name, got, tc.want)
		}
		if got := tc.o.Total(); got < 0 {
			t.Errorf("%s: Total() wrapped negative to %d", tc.name, got)
		}
	}
}
