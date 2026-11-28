package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/auth/authtest"
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/monitoring/monitoringtest"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/jsoncontract"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"altune/go-api/internal/shared/timingtest"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	goredis "github.com/redis/go-redis/v9"
)

func captureLeaderLoopLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	return logging.CaptureRing(t, "info")
}

type alwaysLeader struct{}

func (alwaysLeader) Start(context.Context) {}

func (alwaysLeader) Await(context.Context) bool { return true }

func (alwaysLeader) Shutdown(context.Context) {}

func (alwaysLeader) Counters() leader.Counters { return leader.Counters{} }

func (alwaysLeader) LeaderContext(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}

func leadAlways(ctx context.Context, t *testing.T, r *app.JobRunner) {
	t.Helper()
	r.Lead(ctx, alwaysLeader{})
	t.Cleanup(r.Wait)
}

func awaitPass(t *testing.T, passed <-chan struct{}) {
	t.Helper()
	select {
	case <-passed:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop never ran a pass")
	}
}

func recordNamed(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	for _, rec := range ring.Snapshot() {
		if rec.Message == msg {
			return rec
		}
	}
	t.Fatalf("no %q record was captured", msg)
	return logging.CapturedRecord{}
}

func TestAlertMonitor_ALogInsideAPassCarriesTheJobAndARunID(t *testing.T) {
	ring := captureLeaderLoopLogs(t)
	passed := make(chan struct{}, 1)
	runner := app.JobRunner{Now: clock.Now, NewTicker: clock.NewTicker}
	condition := monitoring.Condition{Key: "probe", Eval: func(ctx context.Context) *monitoringtest.Alert {
		slog.InfoContext(ctx, "alert pass line")
		select {
		case passed <- struct{}{}:
		default:
		}
		return nil
	}}
	monitor, err := app.NewAlertMonitor(monitoringtest.NopNotifier{}, 5*time.Millisecond, []monitoring.Condition{condition})
	if err != nil {
		t.Fatalf("NewAlertMonitor: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runner.Ticker(app.JobAlertMonitor, 5*time.Millisecond, monitor.Pass)
	leadAlways(ctx, t, &runner)
	awaitPass(t, passed)

	rec := recordNamed(t, ring, "alert pass line")
	if rec.Attrs["job"] != string(app.JobAlertMonitor) || rec.Attrs["run_id"] == "" {
		t.Errorf("attrs job=%q run_id=%q, want %q and a run_id", rec.Attrs["job"], rec.Attrs["run_id"], app.JobAlertMonitor)
	}
}

func alertMonitorUsesNopNotifier(t *testing.T, h http.Handler, token string) bool {
	t.Helper()
	code, body := getAs(t, h, "/observe/metrics/live", token)
	if code != http.StatusOK {
		t.Fatalf("operator read of /observe/metrics/live = %d, want 200; body %s", code, body)
	}
	var got struct {
		AlertMonitor *struct {
			NopNotifier bool `json:"nop_notifier"`
		} `json:"alert_monitor"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.AlertMonitor == nil {
		t.Fatalf("live metrics carry no alert_monitor: %s", body)
	}
	return got.AlertMonitor.NopNotifier
}

func TestAlerting_TheMonitorSendsThroughTheConfiguredChannel(t *testing.T) {
	cases := map[string]struct {
		webhook string
		nop     bool
	}{
		"a webhook URL builds a real notifier":    {"http://127.0.0.1:1/alerts", false},
		"no alert channel leaves alerts log-only": {"", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newJWKSFixture(t)
			f.cfg.AlertWebhookURL = tc.webhook
			h := assembledHandler(t, f.cfg)

			if got := alertMonitorUsesNopNotifier(t, h, signedToken(t, f.key, f.operator)); got != tc.nop {
				t.Errorf("alert_monitor.nop_notifier = %v, want %v", got, tc.nop)
			}
		})
	}
}

type jwksFixture struct {
	key      *rsa.PrivateKey
	operator uuid.UUID
	cfg      *config.Config
	jwks     *httptest.Server
	keysDown *atomic.Bool
}

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func newJWKSFixture(t *testing.T) jwksFixture {
	t.Helper()
	key := newRSAKey(t)
	pub, err := jwk.FromRaw(key.PublicKey)
	if err != nil {
		t.Fatalf("jwk from public key: %v", err)
	}
	_ = pub.Set(jwk.KeyIDKey, "k1")
	_ = pub.Set(jwk.AlgorithmKey, jwa.RS256)
	set := jwk.NewSet()
	_ = set.AddKey(pub)
	keysDown := &atomic.Bool{}
	setJSON, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("encode the key set: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if keysDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(setJSON)
	}))
	t.Cleanup(srv.Close)
	operator := uuid.New()
	return jwksFixture{key: key, operator: operator, jwks: srv, keysDown: keysDown, cfg: &config.Config{
		MusicDir:                       t.TempDir(),
		AcquisitionDownloadConcurrency: 1,
		AcquisitionUpgradeConcurrency:  1,
		SupabaseJWTJWKSURL:             srv.URL,
		SupabaseProjectURL:             "https://p.supabase.co",
		SupabaseJWTAud:                 "authenticated",
		OverseerPrincipalID:            operator.String(),
		BackgroundMaxInFlight:          64,

		AcquisitionFileHostPageTimeout:  10 * time.Second,
		AcquisitionDeezerPreviewTimeout: 20 * time.Second,

		PlaybackNowPlayingLookupTimeout: 3 * time.Second,
		PlaybackErasureScanTimeout:      30 * time.Second,
	}}
}

func jwksConfig(t *testing.T) *config.Config {
	t.Helper()
	return newJWKSFixture(t).cfg
}

func signedToken(t *testing.T, key *rsa.PrivateKey, userID uuid.UUID) string {
	t.Helper()
	return signedTokenWithKeyID(t, key, "k1", userID)
}

func signedTokenWithKeyID(t *testing.T, key *rsa.PrivateKey, keyID string, userID uuid.UUID) string {
	t.Helper()
	tok := jwt.New()
	_ = tok.Set("sub", userID.String())
	_ = tok.Set("iss", "https://p.supabase.co/auth/v1")
	_ = tok.Set("aud", "authenticated")
	_ = tok.Set("iat", time.Now().Add(-time.Minute))
	_ = tok.Set("exp", time.Now().Add(30*time.Minute))
	priv, err := jwk.FromRaw(key)
	if err != nil {
		t.Fatalf("jwk from private key: %v", err)
	}
	_ = priv.Set(jwk.KeyIDKey, keyID)
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, priv))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return string(signed)
}

func assembledHandler(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown(context.Background())
	})
	return assembly.Handler()
}

func getAs(t *testing.T, h http.Handler, path, token string) (int, []byte) {
	t.Helper()
	return getFrom(t, h, "192.0.2.1:1234", path, token)
}

func getFrom(t *testing.T, h http.Handler, client, path, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = client
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, body
}

func liveAuthMetrics(t *testing.T, h http.Handler, token string) authtest.MetricsSnapshot {
	t.Helper()
	code, body := getAs(t, h, "/observe/metrics/live", token)
	if code != http.StatusOK {
		t.Fatalf("operator read of /observe/metrics/live = %d, want 200; body %s", code, body)
	}
	var got struct {
		Auth authtest.MetricsSnapshot `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got.Auth
}

func TestAuth_APlaintextJWKSURLFailsAssembly(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.SupabaseJWTJWKSURL = "http://jwks.example.com/keys"

	_, err := app.Assemble(t.Context(), cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})

	if err == nil || !strings.HasPrefix(err.Error(), "auth: ") {
		t.Fatalf("Assemble error = %v, want the auth-wrapped refusal of a plaintext JWKS URL", err)
	}
}

func TestAuth_TheConfiguredVerifierAdmitsTheOperatorAndIsProbedForHealth(t *testing.T) {
	f := newJWKSFixture(t)
	h := assembledHandler(t, f.cfg)

	code, body := getAs(t, h, "/observe/health", signedToken(t, f.key, f.operator))

	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		t.Fatalf("GET /observe/health with the operator's token = %d, want the operator admitted; body %s", code, body)
	}
	var got struct {
		Auth string `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Auth != string(monitoringtest.DepUp) {
		t.Errorf("auth dependency = %q, want %q: the verifier the routes use must also be probed for health", got.Auth, monitoringtest.DepUp)
	}
}

func TestAuth_RejectionsAndAnUnreachableKeySetReachLiveMetrics(t *testing.T) {
	f := newJWKSFixture(t)
	f.keysDown.Store(true)
	h := assembledHandler(t, f.cfg)
	operator := signedToken(t, f.key, f.operator)
	before := authtest.MetricsSnapshot{}

	const firstClient, forgingClient = "198.51.100.7:1234", "198.51.100.8:1234"
	if code, body := getFrom(t, h, firstClient, "/observe/metrics/live", operator); code != http.StatusServiceUnavailable {
		t.Fatalf("a token while the key set cannot be fetched: status %d, want 503; body %s", code, body)
	}
	unavailable := int64(1)
	f.keysDown.Store(false)
	var code int
	var body []byte
	timingtest.Within(10*time.Second, func() bool {
		code, body = getFrom(t, h, fmt.Sprintf("203.0.%d.%d:1234", unavailable/250, unavailable%250+1), "/observe/metrics/live", operator)
		if code == http.StatusServiceUnavailable {
			unavailable++
			return false
		}
		return true
	})
	if code != http.StatusOK {
		t.Fatalf("the operator once the key set is back: status %d, want 200 within 10s; body %s", code, body)
	}
	if code, _ := getFrom(t, h, forgingClient, "/observe/metrics/live", signedToken(t, newRSAKey(t), f.operator)); code != http.StatusUnauthorized {
		t.Fatalf("token signed by another key: status %d, want 401", code)
	}

	after := liveAuthMetrics(t, h, operator)
	if after.TokenRejections != before.TokenRejections+1 {
		t.Errorf("auth.token_rejections_total %d, want %d", after.TokenRejections, before.TokenRejections+1)
	}
	reason := string(authtest.ReasonSignatureInvalid)
	if want := before.TokenRejectionsByReason[reason] + 1; after.TokenRejectionsByReason[reason] != want {
		t.Errorf("auth.token_rejections_by_reason_total[%s] %d, want %d", reason, after.TokenRejectionsByReason[reason], want)
	}
	if after.VerifierUnavailable != before.VerifierUnavailable+unavailable {
		t.Errorf("auth.verifier_unavailable_total %d, want %d, one per 503 served", after.VerifierUnavailable, before.VerifierUnavailable+unavailable)
	}
}

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

func TestObserveRoutes_EveryStreamSendsNosniff(t *testing.T) {
	f := newObserveFixture(t, true)

	streams := 0
	for _, route := range observeRoutes(t, f.tree) {
		if !strings.HasSuffix(route.path, "/stream") {
			continue
		}
		streams++
		rec := callObserve(t, f.tree, route.method, route.path, f.principal)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options = %q, want nosniff", route.method, route.path, got)
		}
	}
	if streams == 0 {
		t.Fatal("no observe stream routes found")
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
	DB     monitoringtest.DepStatus `json:"db"`
	Redis  monitoringtest.DepStatus `json:"redis"`
	Auth   monitoringtest.DepStatus `json:"auth"`
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
		assembly.Shutdown(context.Background())
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
	runner := app.JobRunner{Now: clock.Now, NewTicker: clock.NewTicker}
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

	runner.Ticker(app.JobEvalMeter, 5*time.Millisecond, meter.Pass)
	leadAlways(ctx, t, &runner)
	awaitPass(t, passed)

	rec := recordNamed(t, ring, "eval pass line")
	if rec.Attrs["job"] != string(app.JobEvalMeter) || rec.Attrs["run_id"] == "" {
		t.Errorf("attrs job=%q run_id=%q, want %q and a run_id", rec.Attrs["job"], rec.Attrs["run_id"], app.JobEvalMeter)
	}
}

func TestObserveHealth_ProbesTheAssembledDatabaseRedisAndAuth(t *testing.T) {
	redisUp := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = redisUp.Close() })

	got := readObservedHealth(t, app.Handles{Pool: dbtest.Pool(t), Redis: redisUp}, http.StatusOK)

	if got.DB != monitoringtest.DepUp || got.Redis != monitoringtest.DepUp || got.Auth != monitoringtest.DepUp {
		t.Errorf("health = %+v, want db, redis and auth all ok", got)
	}
}

func TestObserveHealth_ReportsADownDatabaseAndRedisWithTheirOwnErrors(t *testing.T) {
	redisDown := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	t.Cleanup(func() { _ = redisDown.Close() })

	got := readObservedHealth(t, app.Handles{Pool: unreachablePool(t), Redis: redisDown}, http.StatusServiceUnavailable)

	if got.DB != monitoringtest.DepDown || got.Redis != monitoringtest.DepDown {
		t.Errorf("health = %+v, want db and redis down", got)
	}
	if got.Detail.DBError == "" || got.Detail.RedisError == "" || got.Detail.DBError == got.Detail.RedisError {
		t.Errorf("detail = %+v, want each probe's own error", got.Detail)
	}
}

func TestObserveHealth_WithoutRedisItIsNotConfigured(t *testing.T) {
	if got := readObservedHealth(t, app.Handles{Pool: unreachablePool(t)}, http.StatusServiceUnavailable); got.Redis != monitoringtest.DepNotConfigured {
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
		Latency struct {
			Routes map[string]struct {
				Count uint64 `json:"count"`
			} `json:"routes"`
		} `json:"latency"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if rl := got.Latency.Routes["/observe/health"]; rl.Count != 1 {
		t.Errorf("latency.routes[/observe/health] = %+v, want the one request the assembled router served; routes %v", rl, got.Latency.Routes)
	}
}

func liveSources(t *testing.T) monitoring.LiveSources {
	t.Helper()
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	alertMonitor, err := monitoring.NewAlertMonitor(monitoringtest.NopNotifier{}, time.Second, time.Now)
	if err != nil {
		t.Fatalf("NewAlertMonitor: %v", err)
	}
	s := app.LiveSourcesOf(telemetry.NewRegistry(), requestmetrics.New())
	s.PoolStats = func() database.PoolStats { return database.PoolStats{} }
	s.EventBus = bus
	s.EventTap = monitoringtest.MustNewEventTap(bus, time.Now)
	s.EventFeed = monitoring.NewEventFeed()
	s.AlertMonitor = alertMonitor
	s.Background = &background.Tracker{}
	return s
}

func servedLive(t *testing.T, s monitoring.LiveSources) []byte {
	t.Helper()
	r := chi.NewRouter()
	monitoring.New(clock.Now, clock.NewTicker, monitoring.Deps{LiveMetrics: monitoring.NewLiveMetrics(s)}).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics/live = %d, want 200; body %s", rec.Code, rec.Body)
	}
	return rec.Body.Bytes()
}

func decodeLive(t *testing.T, raw []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func TestFeatureMetrics_NamesEveryFeaturesSnapshot(t *testing.T) {
	var got map[string]json.RawMessage
	decodeLive(t, servedLive(t, liveSources(t)), &got)

	for _, key := range []string{"auth", "catalog", "playback", "providers", "latency"} {
		if got[key] == nil {
			t.Errorf("live metrics lack %q: %v", key, got)
		}
	}
}

func TestContract_MetricsLiveMatchesFixture(t *testing.T) {
	latency := requestmetrics.New()
	latency.Observe("/v1/tracks/{trackId}", 5*time.Millisecond, 200)
	s := withRegistry(t, liveSources(t), telemetry.NewRegistry(), latency)

	jsoncontract.AssertMatchesFixture(t, filepath.Join("testdata", "contract", "observe_metrics_live.json"), servedLive(t, s))
}

type counterWire struct {
	Auth struct {
		TokenRejections     int64            `json:"token_rejections_total"`
		ByReason            map[string]int64 `json:"token_rejections_by_reason_total"`
		VerifierUnavailable int64            `json:"verifier_unavailable_total"`
		JWKSFetchFailures   int64            `json:"jwks_fetch_failures_total"`
	} `json:"auth"`
	Catalog struct {
		PresignFailures int64 `json:"presign_failures_total"`
	} `json:"catalog"`
	Playback struct {
		EnrichmentFailures       int64 `json:"now_playing_enrichment_failures_total"`
		CorruptStoredState       int64 `json:"corrupt_stored_state_total"`
		QueueStateOpTimeouts     int64 `json:"queue_state_op_timeouts_total"`
		NowPlayingLookupTimeouts int64 `json:"now_playing_lookup_timeouts_total"`
	} `json:"playback"`
}

func TestLive_CarriesAuthCatalogAndPlaybackFailureCounters(t *testing.T) {
	metrics := telemetry.NewRegistry()
	s := liveSources(t)
	s = withRegistry(t, s, metrics, requestmetrics.New())
	var before counterWire
	decodeLive(t, servedLive(t, s), &before)

	auth.NewAuthMetrics(metrics).TokenRejected("signature_invalid")
	auth.NewAuthMetrics(metrics).VerifierUnavailable()
	auth.NewAuthMetrics(metrics).JWKSFetchFailed()
	library.NewAudioStoreMetrics(metrics).PresignFailed()
	for _, counter := range []string{
		"playback_now_playing_enrichment_failures_total",
		"playback_corrupt_stored_state_total",
		"playback_queue_state_op_timeouts_total",
		"playback_now_playing_lookup_timeouts_total",
	} {
		metrics.Counter(counter).Add(1)
	}

	var got counterWire
	decodeLive(t, servedLive(t, s), &got)
	for _, c := range []struct {
		key       string
		got, want int64
	}{
		{"auth.token_rejections_total", got.Auth.TokenRejections, before.Auth.TokenRejections + 1},
		{"auth.token_rejections_by_reason_total[signature_invalid]", got.Auth.ByReason["signature_invalid"], before.Auth.ByReason["signature_invalid"] + 1},
		{"auth.verifier_unavailable_total", got.Auth.VerifierUnavailable, before.Auth.VerifierUnavailable + 1},
		{"auth.jwks_fetch_failures_total", got.Auth.JWKSFetchFailures, before.Auth.JWKSFetchFailures + 1},
		{"catalog.presign_failures_total", got.Catalog.PresignFailures, before.Catalog.PresignFailures + 1},
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

func withRegistry(t *testing.T, base monitoring.LiveSources, metrics *telemetry.Registry, latency *requestmetrics.Registry) monitoring.LiveSources {
	t.Helper()
	s := app.LiveSourcesOf(metrics, latency)
	s.PoolStats, s.EventBus, s.EventTap, s.EventFeed, s.AlertMonitor, s.Background =
		base.PoolStats, base.EventBus, base.EventTap, base.EventFeed, base.AlertMonitor, base.Background
	return s
}

type statusTransport struct{ status int }

func (s statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: s.status, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
}

type providerWire struct {
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

func TestLive_CarriesProviderBreakerAndLatency(t *testing.T) {
	metrics := telemetry.NewRegistry()
	latency := requestmetrics.New()
	s := withRegistry(t, liveSources(t), metrics, latency)
	var before providerWire
	decodeLive(t, servedLive(t, s), &before)

	const secret = "supersecretquery"
	for url, status := range map[string]int{
		"https://api.deezer.com/search?q=" + secret:     http.StatusOK,
		"https://api.spotify.com/v1/search?q=" + secret: http.StatusTooManyRequests,
	} {
		resp, err := search.NewCountingTransport(statusTransport{status}, metrics).RoundTrip(httptest.NewRequest(http.MethodGet, url, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	metrics.Gauge("playback_now_playing_enrichment_breaker_open").Set(1)
	metrics.Counter("playback_now_playing_enrichment_breaker_rejections_total").Add(1)
	defer metrics.Gauge("playback_now_playing_enrichment_breaker_open").Set(0)
	const route = "/v1/live-snapshot-probe/{status}"
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(clock.Now, requestmetrics.Observer(latency.Observe)))
	router.Get(route, func(w http.ResponseWriter, r *http.Request) {
		status, _ := strconv.Atoi(chi.URLParam(r, "status"))
		w.WriteHeader(status)
	})
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusBadGateway} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/live-snapshot-probe/"+strconv.Itoa(status), nil))
	}

	raw := servedLive(t, s)
	var got providerWire
	decodeLive(t, raw, &got)
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
