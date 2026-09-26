package app

import (
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/auth"
	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/observe/eventtap"
	observeHandler "altune/go-api/internal/observe/handler"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/reqmetrics"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	observePrincipalToken = "observe-principal"
	observeStrangerToken  = "observe-stranger"
	observeReadOnlyToken  = "observe-readonly"
)

var observeRouteParam = regexp.MustCompile(`\{[^}]*\}`)

type observeSubjects struct {
	principal shared.UserId
	stranger  shared.UserId
	readOnly  shared.UserId
}

func newObserveSubjects() observeSubjects {
	return observeSubjects{
		principal: shared.NewUserId(uuid.New()),
		stranger:  shared.NewUserId(uuid.New()),
		readOnly:  shared.NewUserId(uuid.New()),
	}
}

func (s observeSubjects) verifier() auth.TokenVerifier {
	byToken := map[string]shared.UserId{
		observePrincipalToken: s.principal,
		observeStrangerToken:  s.stranger,
		observeReadOnlyToken:  s.readOnly,
	}
	return auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		subject, known := byToken[token]
		if !known {
			return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
		}
		return auth.VerifiedToken{UserID: subject, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
}

func observeTestDeps() observeHandler.Deps {
	shutdown := make(chan struct{})
	close(shutdown)
	return observeHandler.Deps{
		Health: func(context.Context) observeHandler.DependencyHealth {
			return observeHandler.DependencyHealth{DB: observeHandler.DepUp, Redis: observeHandler.DepUp, Auth: observeHandler.DepUp}
		},
		Logs:        logging.NewRingBuffer(8),
		Events:      eventtap.NewFeed(),
		Eval:        evalmeter.New(false, 0, nil),
		LiveMetrics: func() observeHandler.LiveMetrics { return observeHandler.LiveMetrics{} },
		Shutdown:    shutdown,
	}
}

func mountedObserveTree(subjects observeSubjects, cfg *config.Config) *chi.Mux {
	r := chi.NewRouter()
	mountObserve(r, subjects.verifier(), observePrincipal(cfg), observeHandler.New(observeTestDeps()))
	return r
}

func callObserve(t *testing.T, srv http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

type observeRoute struct{ method, path string }

func observeRoutes(t *testing.T, tree *chi.Mux) []observeRoute {
	t.Helper()
	var routes []observeRoute
	walk := func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, observeRoute{method: method, path: observeRouteParam.ReplaceAllString(pattern, "x")})
		return nil
	}
	if err := chi.Walk(tree, walk); err != nil {
		t.Fatalf("walk observe routes: %v", err)
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
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		label := route.method + " " + route.path
		assertObserveStatus(t, callObserve(t, tree, route.method, route.path, ""), http.StatusUnauthorized, label+" without a token")
		assertObserveStatus(t, callObserve(t, tree, route.method, route.path, observeStrangerToken), http.StatusForbidden, label+" as another subject")
		assertObserveStatus(t, callObserve(t, tree, http.MethodPost, route.path, observePrincipalToken), http.StatusMethodNotAllowed, "POST "+route.path+" as the principal")
	}
}

func TestObserveRoutes_EveryRouteOnlyRegistersGet(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		if route.method != http.MethodGet {
			t.Errorf("%s %s: observe serves GET only", route.method, route.path)
		}
	}
}

func TestObserveRoutes_EveryJSONRouteSendsNoStoreAndNosniff(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		if strings.HasSuffix(route.path, "/stream") {
			continue
		}
		rec := callObserve(t, tree, route.method, route.path, observePrincipalToken)
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
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		rec := callObserve(t, tree, route.method, route.path, observeStrangerToken)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s denied: X-Content-Type-Options = %q, want nosniff", route.method, route.path, got)
		}
	}
}

func TestObservePrincipal_FallsBackToReadOnlyWhenUnset(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OperatorReadOnlyUserID: subjects.readOnly.String()})

	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observeReadOnlyToken), http.StatusOK, "read-only id with no overseer principal")
	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observeStrangerToken), http.StatusForbidden, "another subject under the fallback")
}

func TestObservePrincipal_SetPrincipalReplacesReadOnly(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{
		OverseerPrincipalID:    subjects.principal.String(),
		OperatorReadOnlyUserID: subjects.readOnly.String(),
	})

	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observePrincipalToken), http.StatusOK, "the overseer principal")
	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observeReadOnlyToken), http.StatusForbidden, "the read-only id once a principal is set")
}

func TestObserveAcquisition_AbsentSchedulerIsANilReader(t *testing.T) {
	if reader := (&App{}).observeAcquisition(); reader != nil {
		t.Errorf("reader = %#v, want a nil interface so the route reports it unavailable", reader)
	}
}

func TestObserveAcquisition_WiredSchedulerIsTheReader(t *testing.T) {
	scheduler := &acqService.BackgroundAcquisitionScheduler{}
	if reader := (&App{scheduler: scheduler}).observeAcquisition(); reader != scheduler {
		t.Errorf("reader = %#v, want the app's scheduler", reader)
	}
}

func TestObservePrincipal_BothUnsetFailsClosed(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{})

	for _, token := range []string{observePrincipalToken, observeStrangerToken, observeReadOnlyToken} {
		assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", token), http.StatusForbidden, token+" with no principal configured")
	}
}

func callObserveAs(t *testing.T, srv http.Handler, method, path, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func mountObserveLiveMetrics(r chi.Router, verifier auth.TokenVerifier, principal shared.UserId, source observeHandler.LiveMetricsSource) {
	mountObserve(r, verifier, principal.String(), observeHandler.New(observeHandler.Deps{LiveMetrics: source}))
}

func readObservedLiveMetrics(t *testing.T, source observeHandler.LiveMetricsSource) []byte {
	t.Helper()
	subjects := newObserveSubjects()
	r := chi.NewRouter()
	mountObserveLiveMetrics(r, subjects.verifier(), subjects.principal, source)
	code, body := callObserveAs(t, r, http.MethodGet, "/observe/metrics/live", observePrincipalToken)
	if code != http.StatusOK {
		t.Fatalf("GET /observe/metrics/live as the principal: status %d, want 200; body %s", code, body)
	}
	return body
}

type observedCounterWire struct {
	Auth struct {
		TokenRejections     int64            `json:"token_rejections_total"`
		ByReason            map[string]int64 `json:"token_rejections_by_reason_total"`
		VerifierUnavailable int64            `json:"verifier_unavailable_total"`
		JWKSFetchFailures   int64            `json:"jwks_fetch_failures_total"`
	} `json:"auth"`
	Catalog struct {
		PresignFailures int64 `json:"presign_failures_total"`
	} `json:"catalog"`
	Feedback struct {
		TrackerCreateFailures int64            `json:"tracker_create_failures_total"`
		ByCause               map[string]int64 `json:"tracker_create_failures_by_cause_total"`
	} `json:"feedback"`
	Playback struct {
		EnrichmentFailures       int64 `json:"now_playing_enrichment_failures_total"`
		CorruptStoredState       int64 `json:"corrupt_stored_state_total"`
		QueueStateOpTimeouts     int64 `json:"queue_state_op_timeouts_total"`
		NowPlayingLookupTimeouts int64 `json:"now_playing_lookup_timeouts_total"`
	} `json:"playback"`
}

func readObservedCounterWire(t *testing.T) observedCounterWire {
	t.Helper()
	var out observedCounterWire
	if err := json.Unmarshal(readObservedLiveMetrics(t, liveMetricsSnapshot), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestObserveLiveMetrics_CarriesAuthCatalogFeedbackAndPlaybackFailureCounters(t *testing.T) {
	before := readObservedCounterWire(t)

	authmetrics.NewExpvarAuthMetrics().TokenRejected("signature_invalid")
	authmetrics.NewExpvarAuthMetrics().VerifierUnavailable()
	authmetrics.NewExpvarAuthMetrics().JWKSFetchFailed()
	catalogmetrics.NewExpvarAudioStoreMetrics().PresignFailed()
	feedbackmetrics.NewExpvarFeedbackMetrics().TrackerCreateFailed("tracker_unavailable")
	pb := playbackmetrics.NewExpvarPlaybackMetrics()
	pb.EnrichmentFailed()
	pb.CorruptStoredState()
	pb.QueueStateOpTimedOut()
	pb.NowPlayingLookupTimedOut()

	got := readObservedCounterWire(t)
	for _, c := range []struct {
		key       string
		got, want int64
	}{
		{"auth.token_rejections_total", got.Auth.TokenRejections, before.Auth.TokenRejections + 1},
		{"auth.token_rejections_by_reason_total[signature_invalid]", got.Auth.ByReason["signature_invalid"], before.Auth.ByReason["signature_invalid"] + 1},
		{"auth.verifier_unavailable_total", got.Auth.VerifierUnavailable, before.Auth.VerifierUnavailable + 1},
		{"auth.jwks_fetch_failures_total", got.Auth.JWKSFetchFailures, before.Auth.JWKSFetchFailures + 1},
		{"catalog.presign_failures_total", got.Catalog.PresignFailures, before.Catalog.PresignFailures + 1},
		{"feedback.tracker_create_failures_total", got.Feedback.TrackerCreateFailures, before.Feedback.TrackerCreateFailures + 1},
		{"feedback.tracker_create_failures_by_cause_total[tracker_unavailable]", got.Feedback.ByCause["tracker_unavailable"], before.Feedback.ByCause["tracker_unavailable"] + 1},
		{"playback.now_playing_enrichment_failures_total", got.Playback.EnrichmentFailures, before.Playback.EnrichmentFailures + 1},
		{"playback.corrupt_stored_state_total", got.Playback.CorruptStoredState, before.Playback.CorruptStoredState + 1},
		{"playback.queue_state_op_timeouts_total", got.Playback.QueueStateOpTimeouts, before.Playback.QueueStateOpTimeouts + 1},
		{"playback.now_playing_lookup_timeouts_total", got.Playback.NowPlayingLookupTimeouts, before.Playback.NowPlayingLookupTimeouts + 1},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.key, c.got, c.want)
		}
	}
}

type observedElection struct {
	fakeElection
	counters leader.Counters
}

func (c *observedElection) Counters() leader.Counters { return c.counters }

func TestObserveLiveMetrics_CarriesSaturationKeys(t *testing.T) {
	bus := events.NewInProcessBus()
	a := &App{
		election: &observedElection{counters: leader.Counters{Failures: 7}},
		eventBus: bus,
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(readObservedLiveMetrics(t, a.liveMetrics), &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"db_pool", "redis_pool", "leader", "event_bus", "latency", "auth", "catalog", "feedback", "playback", "providers"} {
		if _, ok := out[key]; !ok {
			t.Errorf("live metrics missing key %q", key)
		}
	}
	var l leader.Counters
	if err := json.Unmarshal(out["leader"], &l); err != nil {
		t.Fatal(err)
	}
	if l.Failures != 7 {
		t.Errorf("leader failures = %d, want 7", l.Failures)
	}
}

func TestObserveLiveMetrics_ReportsEventBusDrops(t *testing.T) {
	a := &App{eventBus: events.NewInProcessBus()}
	user := shared.NewUserId(uuid.New())
	_, unsubscribe := a.eventBus.Subscribe(user)
	t.Cleanup(unsubscribe)
	for i := 0; i < 10000 && a.eventBus.Dropped() == 0; i++ {
		a.eventBus.Publish(context.Background(), user, "probe", nil)
	}
	if a.eventBus.Dropped() == 0 {
		t.Fatal("precondition: an unread subscriber never overflowed, so the bus dropped nothing")
	}
	var out struct {
		EventBus eventBusStats `json:"event_bus"`
	}
	if err := json.Unmarshal(readObservedLiveMetrics(t, a.liveMetrics), &out); err != nil {
		t.Fatal(err)
	}
	if out.EventBus.Dropped != a.eventBus.Dropped() {
		t.Errorf("event_bus.dropped_total = %d, want the bus's %d", out.EventBus.Dropped, a.eventBus.Dropped())
	}
}

type observedStatusTransport struct{ status int }

func (s observedStatusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: s.status, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
}

type observedProviderWire struct {
	Providers map[string]struct {
		OK    int64 `json:"ok"`
		Quota int64 `json:"quota"`
	} `json:"providers"`
	Playback struct {
		Open       bool  `json:"now_playing_enrichment_breaker_open"`
		Rejections int64 `json:"now_playing_enrichment_breaker_rejections_total"`
	} `json:"playback"`
	Latency struct {
		Routes map[string]struct {
			Count  uint64            `json:"count"`
			Status map[string]uint64 `json:"status"`
		} `json:"routes"`
	} `json:"latency"`
}

func readObservedProviderWire(t *testing.T) (observedProviderWire, []byte) {
	t.Helper()
	raw := readObservedLiveMetrics(t, liveMetricsSnapshot)
	var out observedProviderWire
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, raw
}

func TestObserveLiveMetrics_CarriesProviderBreakerAndLatency(t *testing.T) {
	before, _ := readObservedProviderWire(t)

	const secret = "supersecretquery"
	for url, status := range map[string]int{
		"https://api.deezer.com/search?q=" + secret:     http.StatusOK,
		"https://api.spotify.com/v1/search?q=" + secret: http.StatusTooManyRequests,
	} {
		resp, err := providermetrics.NewCountingTransport(observedStatusTransport{status}).RoundTrip(httptest.NewRequest(http.MethodGet, url, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	pb := playbackmetrics.NewExpvarPlaybackMetrics()
	pb.EnrichmentBreakerOpened()
	pb.EnrichmentBreakerRejected()
	defer pb.EnrichmentBreakerClosed()
	const route = "/v1/observe-live-snapshot-probe/{id}"
	reqmetrics.Observe(route, 4*time.Millisecond, http.StatusOK)
	reqmetrics.Observe(route, time.Millisecond, http.StatusNotFound)
	reqmetrics.Observe(route, time.Millisecond, http.StatusBadGateway)

	got, raw := readObservedProviderWire(t)
	if got.Providers["deezer"].OK != before.Providers["deezer"].OK+1 {
		t.Errorf("providers.deezer.ok = %d, want %d", got.Providers["deezer"].OK, before.Providers["deezer"].OK+1)
	}
	if got.Providers["spotify"].Quota != before.Providers["spotify"].Quota+1 {
		t.Errorf("providers.spotify.quota = %d, want %d", got.Providers["spotify"].Quota, before.Providers["spotify"].Quota+1)
	}
	if !got.Playback.Open || got.Playback.Rejections != before.Playback.Rejections+1 {
		t.Errorf("breaker open=%v rejections=%d, want open and %d", got.Playback.Open, got.Playback.Rejections, before.Playback.Rejections+1)
	}
	rl := got.Latency.Routes[route]
	if rl.Count != 3 || rl.Status["2xx"] != 1 || rl.Status["4xx"] != 1 || rl.Status["5xx"] != 1 {
		t.Errorf("latency.routes[%q] = %+v, want count 3 and one each of 2xx/4xx/5xx", route, rl)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("snapshot leaks query text %q", secret)
	}
}

func TestDepStatus_MatchesObserveWireValues(t *testing.T) {
	pairs := []struct {
		app     DepStatus
		observe observeHandler.DepStatus
	}{
		{DepUp, observeHandler.DepUp},
		{DepNotConfigured, observeHandler.DepNotConfigured},
		{DepDown, observeHandler.DepDown},
	}
	for _, p := range pairs {
		if string(p.app) != string(p.observe) {
			t.Errorf("app %q != observe %q", p.app, p.observe)
		}
	}
}

func TestObserveHealthProbe_MapsStatuses(t *testing.T) {
	got := (&App{}).observeHealthProbe(context.Background())

	if got.DB != observeHandler.DepNotConfigured || got.Redis != observeHandler.DepNotConfigured || got.Auth != observeHandler.DepNotConfigured {
		t.Errorf("statuses = %q/%q/%q, want all not_configured", got.DB, got.Redis, got.Auth)
	}
	if !got.Healthy() {
		t.Error("Healthy() = false for unconfigured dependencies, want true")
	}
}

func TestEvalMeterRunner_NilWhileTheMeterIsDisabled(t *testing.T) {
	if run := (&App{cfg: &config.Config{EvalMeterEnabled: false}}).evalMeterRunner(); run != nil {
		t.Error("runner is non-nil with the eval meter disabled, want nil so the meter treats it as unset")
	}
}

func TestWrapEvalRunner_ErrPassesThroughWithAZeroResult(t *testing.T) {
	wantErr := errors.New("eval runner boom")
	run := wrapEvalRunner(func(context.Context) (EvalResult, error) {
		return EvalResult{}, wantErr
	})

	res, err := run(context.Background())

	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(res, evalmeter.Result{}) {
		t.Errorf("result = %+v, want the zero Result", res)
	}
}

func TestWireObserve_BuildsTheFeedAndMeterItServes(t *testing.T) {
	subjects := newObserveSubjects()
	a := &App{cfg: &config.Config{OverseerPrincipalID: subjects.principal.String()}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := chi.NewRouter()

	a.wireObserve(ctx, r, subjects.verifier(), eventtap.New(events.NewInProcessBus()))

	if a.eventFeed == nil || a.evalMeter == nil {
		t.Fatalf("feed %v, meter %v: wireObserve must build both", a.eventFeed, a.evalMeter)
	}
	registered := false
	for _, job := range a.backgroundStarts {
		registered = registered || job.name == jobEvalMeter
	}
	if !registered {
		t.Error("the eval meter is not registered to start when leader")
	}
	assertObserveStatus(t, callObserve(t, r, http.MethodGet, "/observe/events/stream", observePrincipalToken), http.StatusOK, "event stream over the wired feed")
}

func TestMeterResult_CarriesEveryScorecardField(t *testing.T) {
	res := EvalResult{
		Score:     0.75,
		Baseline:  0.9,
		Regressed: true,
		Errored:   2,
		Queries: []EvalQueryResult{
			{Query: "radiohead", Expect: "Radiohead", Passed: true, Position: 1},
			{Query: "bjork", Expect: "Björk", Passed: false, Position: 7},
		},
	}

	got := meterResult(res)

	want := evalmeter.Result{
		Score:     0.75,
		Baseline:  0.9,
		Regressed: true,
		Errored:   2,
		Queries: []evalmeter.QueryResult{
			{Query: "radiohead", Expect: "Radiohead", Passed: true, Position: 1},
			{Query: "bjork", Expect: "Björk", Passed: false, Position: 7},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("meterResult = %+v, want %+v", got, want)
	}
}
