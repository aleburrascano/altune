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

func TestAdminEvalDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/eval" {
			t.Errorf("stub got path %s, want /observe/eval", r.URL.Path)
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

func TestAdminAcquisitionDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/observe/acquisition" {
			t.Errorf("stub got path %s, want /observe/acquisition", r.URL.Path)
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
	rate, ok := got.SuccessRateSince(goapi.AcquisitionStatus{})
	if !ok || rate != 19.0/20.0 {
		t.Fatalf("SuccessRateSince(zero) = %v (ok=%v), want 0.95", rate, ok)
	}
}

func TestAcquisitionWindowedRateSeesRecentSpike(t *testing.T) {
	base := goapi.AcquisitionStatus{Succeeded: 990, Failed: 10}
	now := goapi.AcquisitionStatus{Succeeded: 990, Failed: 110}

	rate, ok := now.SuccessRateSince(base)
	if !ok {
		t.Fatal("windowed rate undefined despite 100 recent completions")
	}
	if rate != 0 {
		t.Fatalf("windowed rate = %v, want 0 — the recent window is all failures", rate)
	}
}

func TestAcquisitionWindowedRateUndefinedWithNoRecentCompletions(t *testing.T) {
	base := goapi.AcquisitionStatus{Succeeded: 42, Failed: 7}
	if rate, ok := base.SuccessRateSince(base); ok || rate != 0 {
		t.Fatalf("windowed rate over an idle window = %v (ok=%v), want 0 undefined", rate, ok)
	}
}

func TestAcquisitionWindowedRateUndefinedAcrossCounterReset(t *testing.T) {
	base := goapi.AcquisitionStatus{Succeeded: 990, Failed: 10}
	afterRestart := goapi.AcquisitionStatus{Succeeded: 3, Failed: 1}
	if rate, ok := afterRestart.SuccessRateSince(base); ok || rate != 0 {
		t.Fatalf("windowed rate across a reset = %v (ok=%v), want 0 undefined", rate, ok)
	}
}

func TestAcquisitionWindowedRateSurvivesCounterOverflow(t *testing.T) {
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
		rate, ok := a.SuccessRateSince(goapi.AcquisitionStatus{})
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

func TestEvalAgeReportsElapsedSinceLastRun(t *testing.T) {
	ran := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	now := ran.Add(3 * time.Hour)
	e := goapi.EvalStatus{LastRun: &ran}

	age, known := e.Age(now)
	if !known {
		t.Fatal("Age reported unknown for a scored meter with a last_run")
	}
	if age != 3*time.Hour {
		t.Fatalf("Age = %v, want 3h", age)
	}
}

func TestEvalAgeUnknownWhenNeverRun(t *testing.T) {
	if _, known := (goapi.EvalStatus{}).Age(time.Now()); known {
		t.Fatal("Age reported known for a meter that never ran")
	}
}

func TestEvalStaleByAgePastThreshold(t *testing.T) {
	ran := time.Date(2026, 9, 13, 6, 0, 0, 0, time.UTC)
	now := ran.Add(5 * 24 * time.Hour)
	e := goapi.EvalStatus{LastRun: &ran}

	if !e.StaleByAge(now, 12*time.Hour) {
		t.Fatal("a 5-day-old score was not flagged stale past a 12h threshold")
	}
}

func TestEvalFreshWithinThresholdNotStale(t *testing.T) {
	ran := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	now := ran.Add(2 * time.Hour)
	e := goapi.EvalStatus{LastRun: &ran}

	if e.StaleByAge(now, 12*time.Hour) {
		t.Fatal("a 2h-old score was wrongly flagged stale past a 12h threshold")
	}
}

func TestEvalNeverRunIsNotStale(t *testing.T) {
	if (goapi.EvalStatus{}).StaleByAge(time.Now(), 12*time.Hour) {
		t.Fatal("a never-run meter was wrongly flagged stale by age")
	}
}

func TestAdminEvalUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if _, err := newClient(t, url).AdminEval(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("AdminEval against a closed server: err = %v, want source-down", err)
	}
	if _, err := newClient(t, url).AdminAcquisition(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("AdminAcquisition against a closed server: err = %v, want source-down", err)
	}
}

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

func TestAdminEvalMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminEval(context.Background()); err == nil {
		t.Fatal("malformed eval body returned nil error")
	}
}
