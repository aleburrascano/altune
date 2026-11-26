package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

var observeRouteParam = regexp.MustCompile(`\{[^}]*\}`)

type observeFixture struct {
	tree      *chi.Mux
	principal string
	stranger  string
}

func newObserveFixture(t *testing.T, principalConfigured bool) observeFixture {
	t.Helper()
	f := newJWKSFixture(t)
	if !principalConfigured {
		f.cfg.OverseerPrincipalID = ""
	}
	return observeFixture{
		tree:      productionRouterFor(t, f.cfg),
		principal: signedToken(t, f.key, f.operator),
		stranger:  signedToken(t, f.key, uuid.New()),
	}
}

func callObserve(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type observeRoute struct{ method, path string }

func observeRoutes(t *testing.T, tree *chi.Mux) []observeRoute {
	t.Helper()
	var routes []observeRoute
	walk := func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(pattern, "/observe/") {
			routes = append(routes, observeRoute{method: method, path: observeRouteParam.ReplaceAllString(pattern, "x")})
		}
		return nil
	}
	if err := chi.Walk(tree, walk); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("walked no observe routes, so the assertions proved nothing")
	}
	return routes
}

func assertObserveStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("%s: status %d, want %d; body %s", what, rec.Code, want, rec.Body.String())
	}
}

func TestObserveRoutes_EveryRouteHoldsTheGate(t *testing.T) {
	f := newObserveFixture(t, true)

	for _, route := range observeRoutes(t, f.tree) {
		label := route.method + " " + route.path
		assertObserveStatus(t, callObserve(t, f.tree, route.method, route.path, ""), http.StatusUnauthorized, label+" without a token")
		assertObserveStatus(t, callObserve(t, f.tree, route.method, route.path, f.stranger), http.StatusForbidden, label+" as another subject")
		assertObserveStatus(t, callObserve(t, f.tree, http.MethodPost, route.path, f.principal), http.StatusMethodNotAllowed, "POST "+route.path+" as the principal")
	}
}

func TestObserveRoutes_EveryRouteOnlyRegistersGet(t *testing.T) {
	f := newObserveFixture(t, true)

	for _, route := range observeRoutes(t, f.tree) {
		if route.method != http.MethodGet {
			t.Errorf("%s %s: observe serves GET only", route.method, route.path)
		}
	}
}

func TestObserveRoutes_EveryJSONRouteSendsNoStoreAndNosniff(t *testing.T) {
	f := newObserveFixture(t, true)

	for _, route := range observeRoutes(t, f.tree) {
		if strings.HasSuffix(route.path, "/stream") {
			continue
		}
		rec := callObserve(t, f.tree, route.method, route.path, f.principal)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("%s %s as the principal: status %d, want the gate to admit it", route.method, route.path, rec.Code)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options = %q, want nosniff", route.method, route.path, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want no-store", route.method, route.path, got)
		}
	}
}

func TestObserveRoutes_DenialStillSendsNosniff(t *testing.T) {
	f := newObserveFixture(t, true)

	for _, route := range observeRoutes(t, f.tree) {
		rec := callObserve(t, f.tree, route.method, route.path, f.stranger)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s denied: X-Content-Type-Options = %q, want nosniff", route.method, route.path, got)
		}
	}
}

func TestObserveRoutes_TheEventStreamServesThePrincipal(t *testing.T) {
	f := newObserveFixture(t, true)

	assertObserveStatus(t, callObserve(t, f.tree, http.MethodGet, "/observe/events/stream", f.principal), http.StatusOK, "the event stream over the assembled feed")
}

func TestObservePrincipal_TheConfiguredPrincipalIsAdmittedAndNoOneElse(t *testing.T) {
	f := newObserveFixture(t, true)

	assertObserveStatus(t, callObserve(t, f.tree, http.MethodGet, "/observe/metrics/live", f.principal), http.StatusOK, "the overseer principal")
	assertObserveStatus(t, callObserve(t, f.tree, http.MethodGet, "/observe/metrics/live", f.stranger), http.StatusForbidden, "another subject")
}

func TestObservePrincipal_NoneConfiguredFailsClosed(t *testing.T) {
	f := newObserveFixture(t, false)

	for name, token := range map[string]string{"the would-be principal": f.principal, "another subject": f.stranger} {
		assertObserveStatus(t, callObserve(t, f.tree, http.MethodGet, "/observe/metrics/live", token), http.StatusForbidden, name+" with no principal configured")
	}
}

type observedAcquisition struct {
	Paused        bool `json:"paused"`
	QueueCapacity int  `json:"queue_capacity"`
}

func readObservedAcquisition(t *testing.T, enableSource, paused bool) observedAcquisition {
	t.Helper()
	f := newJWKSFixture(t)
	f.cfg.YtMusicEnabled = enableSource
	f.cfg.AcquisitionConcurrency = 3
	f.cfg.AcquisitionPaused = paused
	code, body := getAs(t, assembledHandler(t, f.cfg), "/observe/acquisition", signedToken(t, f.key, f.operator))
	if code != http.StatusOK {
		t.Fatalf("GET /observe/acquisition = %d, want 200; body %s", code, body)
	}
	var got observedAcquisition
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got
}

func TestObserveAcquisition_WithoutASourceThereIsNoSchedulerToRead(t *testing.T) {
	if got := readObservedAcquisition(t, false, false); got.QueueCapacity != 0 {
		t.Errorf("queue_capacity = %d without an acquisition source, want 0", got.QueueCapacity)
	}
}

func TestObserveAcquisition_ReadsTheAssembledScheduler(t *testing.T) {
	if got := readObservedAcquisition(t, true, false); got.QueueCapacity != 3 || got.Paused {
		t.Errorf("acquisition = %+v, want the scheduler's 3 workers, running", got)
	}
}

func TestObserveAcquisition_AcquisitionPausedStartsTheSchedulerPaused(t *testing.T) {
	if got := readObservedAcquisition(t, true, true); !got.Paused {
		t.Errorf("acquisition = %+v, want paused after ACQUISITION_PAUSED=true", got)
	}
}

type observedHealth struct {
	DB     monitoring.DepStatus `json:"db"`
	Redis  monitoring.DepStatus `json:"redis"`
	Auth   monitoring.DepStatus `json:"auth"`
	Detail struct {
		DBError    string `json:"db_error"`
		RedisError string `json:"redis_error"`
	} `json:"detail"`
}

func readObservedHealth(t *testing.T, handles app.Handles, wantCode int) observedHealth {
	t.Helper()
	f := newJWKSFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	handles.Transport = refuseEgress{}
	handles.Tracker = testTracker(t)
	assembly, err := app.Assemble(ctx, f.cfg, handles)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown()
	})
	code, body := getAs(t, assembly.Handler(), "/observe/health", signedToken(t, f.key, f.operator))
	if code != wantCode {
		t.Fatalf("GET /observe/health = %d, want %d; body %s", code, wantCode, body)
	}
	var got observedHealth
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got
}

func TestEvalMeter_ALogInsideAPassCarriesTheJobAndARunID(t *testing.T) {
	ring := captureLeaderLoopLogs(t)
	passed := make(chan struct{}, 1)
	var runner jobs.Runner
	meter, err := app.NewEvalMeter(&runner, func(ctx context.Context) (monitoring.EvalMeterResult, error) {
		slog.InfoContext(ctx, "eval pass line")
		passed <- struct{}{}
		return monitoring.EvalMeterResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	meter.Start(ctx)
	awaitPass(t, passed)

	rec := recordNamed(t, ring, "eval pass line")
	if rec.Attrs["job"] != string(jobs.EvalMeter) || rec.Attrs["run_id"] == "" {
		t.Errorf("attrs job=%q run_id=%q, want %q and a run_id", rec.Attrs["job"], rec.Attrs["run_id"], jobs.EvalMeter)
	}
}

func TestObserveHealth_ProbesTheAssembledDatabaseRedisAndAuth(t *testing.T) {
	redisUp := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = redisUp.Close() })

	got := readObservedHealth(t, app.Handles{Pool: integrationPool(t), Redis: redisUp}, http.StatusOK)

	if got.DB != monitoring.DepUp || got.Redis != monitoring.DepUp || got.Auth != monitoring.DepUp {
		t.Errorf("health = %+v, want db, redis and auth all ok", got)
	}
}

func TestObserveHealth_ReportsADownDatabaseAndRedisWithTheirOwnErrors(t *testing.T) {
	redisDown := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	t.Cleanup(func() { _ = redisDown.Close() })

	got := readObservedHealth(t, app.Handles{Pool: unreachablePool(t), Redis: redisDown}, http.StatusServiceUnavailable)

	if got.DB != monitoring.DepDown || got.Redis != monitoring.DepDown {
		t.Errorf("health = %+v, want db and redis down", got)
	}
	if got.Detail.DBError == "" || got.Detail.RedisError == "" || got.Detail.DBError == got.Detail.RedisError {
		t.Errorf("detail = %+v, want each probe's own error", got.Detail)
	}
}

func TestObserveHealth_WithoutRedisItIsNotConfigured(t *testing.T) {
	if got := readObservedHealth(t, app.Handles{Pool: unreachablePool(t)}, http.StatusServiceUnavailable); got.Redis != monitoring.DepNotConfigured {
		t.Errorf("redis = %q without a Redis handle, want not_configured", got.Redis)
	}
}

func TestObserveLiveMetrics_CarriesTheLatencyTheAssembledRouterRecorded(t *testing.T) {
	f := newJWKSFixture(t)
	h := assembledHandler(t, f.cfg)
	token := signedToken(t, f.key, f.operator)

	if code, body := getAs(t, h, "/observe/health", token); code == http.StatusUnauthorized || code == http.StatusForbidden {
		t.Fatalf("operator read of /observe/health = %d; body %s", code, body)
	}
	code, body := getAs(t, h, "/observe/metrics/live", token)

	if code != http.StatusOK {
		t.Fatalf("operator read of /observe/metrics/live = %d, want 200; body %s", code, body)
	}
	var got struct {
		Latency requestmetrics.Snapshot `json:"latency"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if rl := got.Latency.Routes["/observe/health"]; rl.Count != 1 {
		t.Errorf("latency.routes[/observe/health] = %+v, want the one request the assembled router served; routes %v", rl, got.Latency.Routes)
	}
}
