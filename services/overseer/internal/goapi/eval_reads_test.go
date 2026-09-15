package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const adminEvalBody = `{
	"enabled": true,
	"paused": false,
	"state": "ok",
	"score": 0.81,
	"baseline": 0.75,
	"last_run": "2026-09-15T10:04:05Z",
	"queries": [
		{"query": "miles davis", "expect": "kind of blue", "passed": true, "position": 1},
		{"query": "radiohead", "expect": "ok computer", "passed": false, "position": 7}
	]
}`

const adminAcquisitionBody = `{
	"in_flight": 2,
	"succeeded": 19,
	"failed": 1,
	"rejected": 3,
	"queue_depth": 4,
	"queue_capacity": 64
}`

// TestAdminEvalDecodesStubbedResponse is the core Done proof for the eval read:
// the client hits a stubbed /admin/eval and gets a fully decoded eval-meter
// status, including the score-vs-baseline and the per-query results.
func TestAdminEvalDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/admin/eval" {
			t.Errorf("stub got path %s, want /admin/eval", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminEvalBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminEval(context.Background())
	if err != nil {
		t.Fatalf("AdminEval: unexpected error: %v", err)
	}
	if !got.Scored() {
		t.Fatal("Scored() = false for a scored eval status")
	}
	if got.Score == nil || *got.Score != 0.81 || got.Baseline == nil || *got.Baseline != 0.75 {
		t.Fatalf("score/baseline = %+v, want 0.81/0.75", got)
	}
	if got.State != "ok" || !got.Enabled || got.Paused {
		t.Fatalf("meta = %+v, want state=ok enabled paused=false", got)
	}
	if len(got.Queries) != 2 || got.Queries[0].Query != "miles davis" || got.Queries[1].Passed {
		t.Fatalf("queries decoded wrong: %+v", got.Queries)
	}
	want := time.Date(2026, 9, 15, 10, 4, 5, 0, time.UTC)
	if got.LastRun == nil || !got.LastRun.Equal(want) {
		t.Fatalf("LastRun = %v, want %v", got.LastRun, want)
	}
}

// TestAdminEvalNoDataLeavesScoreNil proves the "not scored yet" state decodes to
// a nil Score rather than a spurious zero the panel would render as 0%.
func TestAdminEvalNoDataLeavesScoreNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"enabled":true,"state":"no_data"}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminEval(context.Background())
	if err != nil {
		t.Fatalf("AdminEval: %v", err)
	}
	if got.Scored() || got.Score != nil {
		t.Fatalf("Score should be nil for no_data, got %+v", got)
	}
}

// TestAdminAcquisitionDecodesStubbedResponse is the core Done proof for the
// acquisition read: the counters and gauges decode and the success rate is
// computed from succeeded/(succeeded+failed).
func TestAdminAcquisitionDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/acquisition" {
			t.Errorf("stub got path %s, want /admin/acquisition", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminAcquisitionBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminAcquisition(context.Background())
	if err != nil {
		t.Fatalf("AdminAcquisition: unexpected error: %v", err)
	}
	if got.Succeeded != 19 || got.Failed != 1 || got.Rejected != 3 {
		t.Fatalf("counters = %+v, want succeeded=19 failed=1 rejected=3", got)
	}
	if got.InFlight != 2 || got.QueueDepth != 4 || got.QueueCapacity != 64 {
		t.Fatalf("gauges = %+v, want in_flight=2 depth=4 cap=64", got)
	}
	rate, ok := got.SuccessRate()
	if !ok || rate != 19.0/20.0 {
		t.Fatalf("SuccessRate = %v (ok=%v), want 0.95", rate, ok)
	}
}

// TestAdminAcquisitionSuccessRateUndefinedWithNoCompletions proves the rate is
// undefined (not a divide-by-zero, not a misleading 0%) when nothing has
// completed, so the bucket can render "no data" instead.
func TestAdminAcquisitionSuccessRateUndefinedWithNoCompletions(t *testing.T) {
	a := goapi.AcquisitionStatus{Rejected: 5, QueueDepth: 2}
	if rate, ok := a.SuccessRate(); ok || rate != 0 {
		t.Fatalf("SuccessRate with no completions = %v (ok=%v), want 0 undefined", rate, ok)
	}
}

// TestAdminAcquisitionSuccessRateSurvivesCounterOverflow proves a hostile or
// corrupt go-api response with counters near the uint64 ceiling cannot wrap the
// completed-jobs sum: a raw uint64 add would wrap 2^63+2^63 to 0 (spurious "no
// data") and (2^64-1)+5 to 4 (a rate far above 100%). Taken in float64 the sum
// never wraps, so the rate stays defined and bounded to [0,1].
func TestAdminAcquisitionSuccessRateSurvivesCounterOverflow(t *testing.T) {
	const maxU64 = ^uint64(0)
	for _, tc := range []struct {
		name             string
		succeeded, faild uint64
		wantOK           bool
		wantRate         float64
	}{
		{"exact wrap to zero", 1 << 63, 1 << 63, true, 0.5},
		{"partial wrap", maxU64, 5, true, float64(maxU64) / (float64(maxU64) + 5)},
		{"both at ceiling", maxU64, maxU64, true, 0.5},
	} {
		a := goapi.AcquisitionStatus{Succeeded: tc.succeeded, Failed: tc.faild}
		rate, ok := a.SuccessRate()
		if ok != tc.wantOK {
			t.Fatalf("%s: ok = %v, want %v", tc.name, ok, tc.wantOK)
		}
		if rate != tc.wantRate {
			t.Fatalf("%s: rate = %v, want %v", tc.name, rate, tc.wantRate)
		}
		if rate < 0 || rate > 1 {
			t.Fatalf("%s: rate %v escaped [0,1]", tc.name, rate)
		}
	}
}

// TestAdminEvalUnreachableYieldsSourceDown proves the degrade path: an
// unreachable go-api yields the typed source-down error the bucket branches on to
// serve last-known state flagged stale.
func TestAdminEvalUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	if _, err := newClient(t, url).AdminEval(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("AdminEval against a closed server: err = %v, want source-down", err)
	}
	if _, err := newClient(t, url).AdminAcquisition(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("AdminAcquisition against a closed server: err = %v, want source-down", err)
	}
}

// TestAdminEvalForbiddenYieldsAPIError proves the auth-rejection path: a
// non-operator principal is a reachable-but-refused read, so it surfaces as an
// APIError, NOT source-down. go-api is up; it said no.
func TestAdminEvalForbiddenYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).AdminEval(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("403 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("error %v (%T) is not a 403 APIError", err, err)
	}
}

// TestAdminEvalAttachesOperatorBearer proves both new reads authenticate as the
// operator principal: without the bearer go-api's OperatorOnly guard would 403.
func TestAdminEvalAttachesOperatorBearer(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*goapi.Client) error
	}{
		{"eval", func(c *goapi.Client) error { _, e := c.AdminEval(context.Background()); return e }},
		{"acquisition", func(c *goapi.Client) error { _, e := c.AdminAcquisition(context.Background()); return e }},
	} {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{}`))
		}))
		if err := tc.call(newClient(t, srv.URL)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		srv.Close()
		if want := "Bearer " + testToken; gotAuth != want {
			t.Fatalf("%s Authorization = %q, want %q", tc.name, gotAuth, want)
		}
	}
}

// TestAdminEvalMalformedBodyIsDecodeError proves a garbage 2xx body fails as a
// decode error, not a silent zero-value status the bucket would mistake for a
// healthy, unscored meter.
func TestAdminEvalMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminEval(context.Background()); err == nil {
		t.Fatal("malformed eval body returned nil error")
	}
}
