package app

import (
	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/reqmetrics"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// TestDepStatus_MatchesAdminWireValues pins the app-owned DepStatus values to
// the admin handler's, since adminHealthProbe maps one to the other by
// conversion and the handler's values are the /admin/health wire contract.
func TestDepStatus_MatchesAdminWireValues(t *testing.T) {
	pairs := []struct {
		app   DepStatus
		admin adminHandler.DepStatus
	}{
		{DepUp, adminHandler.DepUp},
		{DepNotConfigured, adminHandler.DepNotConfigured},
		{DepDown, adminHandler.DepDown},
	}
	for _, p := range pairs {
		if string(p.app) != string(p.admin) {
			t.Errorf("app %q != admin %q", p.app, p.admin)
		}
	}
}

func TestAdminHealthProbe_MapsStatuses(t *testing.T) {
	a := &App{}

	got := a.adminHealthProbe(context.Background())

	if got.DB != adminHandler.DepNotConfigured || got.Redis != adminHandler.DepNotConfigured || got.Auth != adminHandler.DepNotConfigured {
		t.Errorf("statuses = %q/%q/%q, want all not_configured", got.DB, got.Redis, got.Auth)
	}
	if !got.Healthy() {
		t.Error("Healthy() = false for unconfigured dependencies, want true")
	}
}

const (
	jobsTestJob      = "discovery metrics rollup"
	jobsTestJobPath  = "/admin/jobs/discovery%20metrics%20rollup"
	jobsTestInterval = 2 * time.Millisecond
	operatorToken    = "operator-token"
	strangerToken    = "stranger-token"
)

type jobDTO struct {
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Failures    int64      `json:"failures"`
	Skipped     int64      `json:"skipped"`
	LastSuccess *time.Time `json:"last_success"`
}

// jobsAdminServer mounts the production /admin tree (bearer auth, then the
// operator gate) with the production job adapter over a, authenticating
// operatorToken as the operator and strangerToken as some other user.
func jobsAdminServer(t *testing.T, a *App, withJobs bool) http.Handler {
	t.Helper()
	operator := shared.NewUserId(uuid.New())
	stranger := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		switch token {
		case operatorToken:
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		case strangerToken:
			return auth.VerifiedToken{UserID: stranger, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	h := adminHandler.New(nil, nil)
	if withJobs {
		h = h.WithJobs(adminJobs{app: a})
	}
	r := chi.NewRouter()
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, h)
	return r
}

func callAdmin(t *testing.T, srv http.Handler, method, path, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func flipJob(t *testing.T, srv http.Handler, action string) jobDTO {
	t.Helper()
	code, body := callAdmin(t, srv, http.MethodPost, jobsTestJobPath+"/"+action, operatorToken)
	if code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200; body %s", action, code, body)
	}
	var st jobDTO
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decode %s response %q: %v", action, body, err)
	}
	return st
}

func listedJob(t *testing.T, srv http.Handler) jobDTO {
	t.Helper()
	code, body := callAdmin(t, srv, http.MethodGet, "/admin/jobs", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("GET /admin/jobs = %d, want 200; body %s", code, body)
	}
	var out struct {
		Jobs []jobDTO `json:"jobs"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode jobs %q: %v", body, err)
	}
	for _, j := range out.Jobs {
		if j.Name == jobsTestJob {
			return j
		}
	}
	t.Fatalf("job %q not listed: %s", jobsTestJob, body)
	return jobDTO{}
}

func waitForJob(t *testing.T, srv http.Handler, what string, cond func(jobDTO) bool) jobDTO {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		j := listedJob(t, srv)
		if cond(j) {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: job stayed %+v", what, j)
		}
		time.Sleep(jobsTestInterval)
	}
}

// startCountingJob registers a ticker through the production startTicker path
// and runs the leader-acquired start, as startBackgroundWhenLeader does.
func startCountingJob(t *testing.T, a *App, runs *atomic.Int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a.startTicker(ctx, jobsTestJob, jobsTestInterval, func(context.Context) error {
		runs.Add(1)
		return nil
	})
	t.Cleanup(func() { cancel(); a.wg.Wait() })
	for _, job := range a.backgroundStarts {
		job.start(ctx)
	}
}

// TestAdminJobs_DisableSkipsTicksAndEnableResumes is the regression for #1020:
// the background-job kill switch and health must be reachable through the
// operator-gated admin router, so disabling a job stops its work (each tick
// recorded as skipped), and re-enabling resumes it, without a redeploy.
func TestAdminJobs_DisableSkipsTicksAndEnableResumes(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	var runs atomic.Int64
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	startCountingJob(t, a, &runs)

	waitForJob(t, srv, "job never succeeded", func(j jobDTO) bool { return j.Enabled && j.LastSuccess != nil })

	if st := flipJob(t, srv, "disable"); st.Enabled {
		t.Fatalf("disable response still enabled: %+v", st)
	}
	time.Sleep(20 * time.Millisecond) // let a tick already past the gate finish
	paused := runs.Load()
	skippedFrom := listedJob(t, srv).Skipped
	j := waitForJob(t, srv, "disabled ticks not recorded as skipped", func(j jobDTO) bool { return j.Skipped >= skippedFrom+3 })
	if j.Enabled {
		t.Fatalf("GET /admin/jobs reports disabled job enabled: %+v", j)
	}
	if got := runs.Load(); got != paused {
		t.Fatalf("disabled job kept running: runs %d -> %d", paused, got)
	}

	if st := flipJob(t, srv, "enable"); !st.Enabled {
		t.Fatalf("enable response still disabled: %+v", st)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() <= paused+2 {
		if time.Now().After(deadline) {
			t.Fatalf("re-enabled job did not resume: runs stuck at %d", runs.Load())
		}
		time.Sleep(jobsTestInterval)
	}
	if !listedJob(t, srv).Enabled {
		t.Fatal("GET /admin/jobs reports re-enabled job disabled")
	}

	assertKillSwitchAudit(t, logs.String())
}

func assertKillSwitchAudit(t *testing.T, logged string) {
	t.Helper()
	var paused []any
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil || m["msg"] != "admin.kill_switch" {
			continue
		}
		if m["loop"] != "background_job" || m["job"] != jobsTestJob || m["actor"] == "unknown" || m["actor"] == nil {
			t.Errorf("malformed kill-switch audit record: %v", m)
		}
		paused = append(paused, m["paused"])
	}
	if len(paused) != 2 || paused[0] != true || paused[1] != false {
		t.Fatalf("kill-switch audit records paused = %v, want [true false]; logs:\n%s", paused, logged)
	}
}

func TestAdminJobs_OperatorOnly(t *testing.T) {
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	a.job(jobsTestJob)

	requests := []struct{ method, path string }{
		{http.MethodGet, "/admin/jobs"},
		{http.MethodPost, jobsTestJobPath + "/enable"},
		{http.MethodPost, jobsTestJobPath + "/disable"}, // last, so a leaky gate leaves it disabled
	}
	for _, rq := range requests {
		if code, _ := callAdmin(t, srv, rq.method, rq.path, ""); code != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated = %d, want 401", rq.method, rq.path, code)
		}
		if code, _ := callAdmin(t, srv, rq.method, rq.path, strangerToken); code != http.StatusForbidden {
			t.Errorf("%s %s non-operator = %d, want 403", rq.method, rq.path, code)
		}
	}
	if h := findJobHealth(t, a.JobHealth(), jobsTestJob); !h.Enabled {
		t.Fatalf("rejected caller flipped the kill switch: %+v", h)
	}
}

func TestAdminJobs_UnknownJobAndUnwired(t *testing.T) {
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	code, body := callAdmin(t, srv, http.MethodPost, "/admin/jobs/nope/disable", operatorToken)
	if code != http.StatusNotFound || !strings.Contains(string(body), "admin.job_not_found") {
		t.Errorf("unknown job = %d %s, want 404 admin.job_not_found", code, body)
	}
	if len(a.JobHealth()) != 0 {
		t.Errorf("unknown job name registered a job: %+v", a.JobHealth())
	}

	unwired := jobsAdminServer(t, a, false)
	for _, path := range []string{"/admin/jobs", "/admin/jobs/nope/disable"} {
		method := http.MethodPost
		if path == "/admin/jobs" {
			method = http.MethodGet
		}
		code, body := callAdmin(t, unwired, method, path, operatorToken)
		if code != http.StatusServiceUnavailable || !strings.Contains(string(body), "admin.jobs_unavailable") {
			t.Errorf("unwired %s %s = %d %s, want 503 admin.jobs_unavailable", method, path, code, body)
		}
	}
}

func mountedAdminTree(t *testing.T) *chi.Mux {
	t.Helper()
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	r := chi.NewRouter()
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, adminHandler.New(nil, nil))
	return r
}

func headersOf(t *testing.T, srv http.Handler, method, path string) http.Header {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Header()
}

// Every route under /admin answers an authenticated operator, so none of them
// may be stored by a shared cache or content-sniffed. Walking the tree is what
// makes a route added later inherit the rule instead of opting into it (#1995).
func TestMountAdmin_EveryRouteSendsNoStoreAndNosniff(t *testing.T) {
	tree := mountedAdminTree(t)

	walked := 0
	err := chi.Walk(tree, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		walked++
		headers := headersOf(t, tree, method, route)
		if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options = %q, want nosniff", method, route, got)
		}
		if got := headers.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want no-store", method, route, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk admin routes: %v", err)
	}
	if walked == 0 {
		t.Fatal("walked no admin routes, so the assertions proved nothing")
	}
}

type liveCounterWire struct {
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

func readLiveCounterWire(t *testing.T) liveCounterWire {
	t.Helper()
	raw, err := json.Marshal(liveMetricsSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var out liveCounterWire
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLiveMetricsSnapshot_CarriesAuthCatalogFeedbackAndPlaybackFailureCounters(t *testing.T) {
	before := readLiveCounterWire(t)

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

	got := readLiveCounterWire(t)
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

type counterElection struct {
	fakeElection
	counters leader.Counters
}

func (c *counterElection) Counters() leader.Counters { return c.counters }

func TestLiveMetrics_CarriesSaturationKeys(t *testing.T) {
	a := &App{
		election: &counterElection{counters: leader.Counters{Failures: 7}},
		eventBus: events.NewInProcessBus(),
	}
	raw, err := json.Marshal(a.liveMetrics())
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"db_pool", "redis_pool", "leader", "event_bus", "latency"} {
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

type statusTransport struct{ status int }

func (s statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: s.status, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
}

func TestLiveMetricsSnapshot_CarriesProviderBreakerAndLatency(t *testing.T) {
	read := func() (out struct {
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
	}, raw []byte,
	) {
		raw, err := json.Marshal(liveMetricsSnapshot())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out, raw
	}
	before, _ := read()

	const secret = "supersecretquery"
	for url, status := range map[string]int{
		"https://api.deezer.com/search?q=" + secret:     http.StatusOK,
		"https://api.spotify.com/v1/search?q=" + secret: http.StatusTooManyRequests,
	} {
		resp, err := providermetrics.NewCountingTransport(statusTransport{status}).RoundTrip(httptest.NewRequest(http.MethodGet, url, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	pb := playbackmetrics.NewExpvarPlaybackMetrics()
	pb.EnrichmentBreakerOpened()
	pb.EnrichmentBreakerRejected()
	defer pb.EnrichmentBreakerClosed()
	const route = "/v1/live-snapshot-probe/{id}"
	reqmetrics.Observe(route, 4*time.Millisecond, http.StatusOK)
	reqmetrics.Observe(route, time.Millisecond, http.StatusNotFound)
	reqmetrics.Observe(route, time.Millisecond, http.StatusBadGateway)

	got, raw := read()
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
