package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/history"
	"altune/overseer/internal/shell"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestBucketsAPIReturnsSortedSnapshots(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "usage", state: core.StateLive})
	reg.Register(stubBucket{id: "liveactivity", state: core.StateLive})
	srv := newServer(reg)

	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want json", ct)
	}
	var resp struct {
		Buckets []core.Snapshot `json:"buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2", len(resp.Buckets))
	}
	if resp.Buckets[0].ID != "liveactivity" || resp.Buckets[1].ID != "usage" {
		t.Errorf("order = %s,%s, want liveactivity,usage", resp.Buckets[0].ID, resp.Buckets[1].ID)
	}
}

func TestSourceDownBucketStillServedAndShellUp(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{stubBucket{id: "reliability", state: core.StateSourceDown}}}
	srv := newServer(reg)

	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	var resp struct {
		Buckets []core.Snapshot `json:"buckets"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Buckets) != 1 || resp.Buckets[0].State != core.StateSourceDown {
		t.Fatalf("want a single source_down snapshot, got %+v", resp.Buckets)
	}
	if h := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil)); h.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200 while a source is down", h.Code)
	}
	if s := do(srv, httptest.NewRequest(http.MethodGet, "/", nil)); s.Code != http.StatusOK {
		t.Errorf("SPA / = %d, want 200 while a source is down", s.Code)
	}
}

func TestPanickingBucketContained(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{panicBucket{}, stubBucket{id: "ok", state: core.StateLive}}}
	srv := newServer(reg)

	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite a panicking bucket", rec.Code)
	}
	var resp struct {
		Buckets []core.Snapshot `json:"buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	byID := map[string]core.Snapshot{}
	for _, s := range resp.Buckets {
		byID[s.ID] = s
	}
	if byID["boom"].State != core.StateSourceDown {
		t.Errorf("panicking bucket state = %q, want source_down (contained)", byID["boom"].State)
	}
	if byID["ok"].State != core.StateLive {
		t.Errorf("healthy sibling state = %q, want live", byID["ok"].State)
	}
}

func TestStreamIsGuarded(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})

	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/api/stream", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauth /api/stream = %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
	req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
	if rec := do(srv, req); rec.Code != http.StatusForbidden {
		t.Errorf("non-owner /api/stream = %d, want 403", rec.Code)
	}
}

func TestStreamEmitsFrames(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "liveactivity", state: core.StateLive}}})

	ctx, cancel := context.WithCancel(context.Background())
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/stream", nil)).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	sawData := false
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data: ") {
			sawData = true
			var snap core.Snapshot
			if err := json.Unmarshal([]byte(strings.TrimPrefix(sc.Text(), "data: ")), &snap); err != nil {
				t.Fatalf("frame is not a Snapshot JSON: %v", err)
			}
			if snap.ID != "liveactivity" {
				t.Errorf("frame id = %q, want liveactivity", snap.ID)
			}
		}
	}
	if !sawData {
		t.Errorf("no SSE data frame emitted:\n%s", rec.Body.String())
	}
}

func TestOpenRoutes(t *testing.T) {
	srv := newServer(fixedRegistry{})

	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil)); rec.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200", rec.Code)
	}
	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/", nil)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Overseer") {
		t.Errorf("/ = %d body=%q, want 200 SPA index", rec.Code, rec.Body.String())
	}
	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)); rec.Code != http.StatusOK {
		t.Errorf("/assets/app.js = %d, want 200", rec.Code)
	}
	cfg := do(srv, httptest.NewRequest(http.MethodGet, "/config.json", nil))
	if cfg.Code != http.StatusOK {
		t.Fatalf("/config.json = %d, want 200", cfg.Code)
	}
	var c struct {
		SupabaseURL     string `json:"supabaseUrl"`
		SupabaseAnonKey string `json:"supabaseAnonKey"`
	}
	if err := json.Unmarshal(cfg.Body.Bytes(), &c); err != nil {
		t.Fatalf("config unmarshal: %v", err)
	}
	if c.SupabaseURL != "https://x.supabase.co" || c.SupabaseAnonKey != "anon-key" {
		t.Errorf("config = %+v, want the public Supabase client values", c)
	}
}

func TestOnlyGetRoutes(t *testing.T) {
	srv := newServer(fixedRegistry{})
	routes, ok := srv.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi.Routes; cannot walk it")
	}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			t.Errorf("route %s %s is not a GET; observe-only forbids a mutating route", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
}

func TestHealthIsNon200WhenTheCollectLoopStalled(t *testing.T) {
	lastCycle := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	srv := newServer(fixedRegistry{}, fixedCollectStatus(shell.CollectStatus{
		Healthy:   false,
		LastCycle: lastCycle,
		OK:        2,
		Failed:    1,
	}))

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health = %d with a stalled collect loop, want 503", rec.Code)
	}
	var body struct {
		Status        string `json:"status"`
		LastCycle     string `json:"last_cycle"`
		BucketsOK     int    `json:"buckets_ok"`
		BucketsFailed int    `json:"buckets_failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health unmarshal: %v", err)
	}
	if body.Status == "ok" {
		t.Errorf("health status = %q, want a stalled status", body.Status)
	}
	if body.LastCycle != lastCycle.Format(time.RFC3339) || body.BucketsOK != 2 || body.BucketsFailed != 1 {
		t.Errorf("health body = %+v, want the loop's last cycle and counts for diagnosis", body)
	}
}

func TestHealthStaysOKWhenEveryBucketFailedButTheLoopRan(t *testing.T) {
	srv := newServer(fixedRegistry{}, fixedCollectStatus(shell.CollectStatus{
		Healthy:   true,
		LastCycle: time.Now(),
		OK:        0,
		Failed:    3,
	}))

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health = %d with a live loop and every source down, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("health body = %s, want status ok", rec.Body.String())
	}
}

func TestSPAFallback(t *testing.T) {
	srv := newServer(fixedRegistry{})
	rec := do(srv, httptest.NewRequest(http.MethodGet, "/some/deep/link", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Overseer") {
		t.Errorf("deep link = %d, want 200 SPA index fallback", rec.Code)
	}
}

func TestNonOwnerTokenForbidden(t *testing.T) {
	srv := newServer(fixedRegistry{})
	for _, path := range []string{"/api/buckets", "/api/stream"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		if rec := do(srv, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s with non-owner token = %d, want 403", path, rec.Code)
		}
	}
}

func TestMissingOrInvalidTokenUnauthorized(t *testing.T) {
	srv := newServer(fixedRegistry{})
	cases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"no header", func(*http.Request) {}},
		{"empty bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }},
		{"no scheme", func(r *http.Request) { r.Header.Set("Authorization", ownerToken) }},
		{"basic scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+ownerToken) }},
		{"token as query", func(r *http.Request) { r.URL.RawQuery = "token=" + ownerToken }},
		{"unverifiable token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer forged.jwt.value") }},
		{"cookie is not accepted", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "overseer_token", Value: ownerToken}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
			tc.mutate(req)
			if rec := do(srv, req); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: got %d, want 401", tc.name, rec.Code)
			}
		})
	}
}

func TestNoRouteSetsCookie(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/config.json", nil),
		httptest.NewRequest(http.MethodGet, "/", nil),
		httptest.NewRequest(http.MethodGet, "/assets/app.js", nil),
		withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)),
		httptest.NewRequest(http.MethodGet, "/api/buckets", nil),
	}
	for _, req := range requests {
		rec := do(srv, req)
		if sc := rec.Header().Values("Set-Cookie"); len(sc) != 0 {
			t.Errorf("%s emitted Set-Cookie %v, want none", req.URL.Path, sc)
		}
	}
}

func TestFailsClosedWithoutOwnerID(t *testing.T) {
	srv := shell.NewHandler(fixedRegistry{},
		shell.WithVerifier(newFakeVerifier()),
		shell.WithOwnerUserID(""),
		shell.WithStreamInterval(10*time.Millisecond),
	).Router()
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	if rec := do(srv, req); rec.Code != http.StatusForbidden {
		t.Fatalf("no-owner-id server = %d, want 403 (fail closed)", rec.Code)
	}
}

func TestOwnerTokenPasses(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	if rec := do(srv, req); rec.Code != http.StatusOK {
		t.Fatalf("owner GET /api/buckets = %d, want 200", rec.Code)
	}
}

func TestOwnerAccessIsAudited(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil))
	buf := captureSlog(t)

	do(srv, req)

	records := logRecordsNamed(buf, "overseer.auth.access")
	if len(records) != 1 {
		t.Fatalf("owner GET /api/buckets emitted %d access records, want 1", len(records))
	}
	rec := records[0]
	for field, want := range map[string]any{
		"level":   "INFO",
		"subject": ownerID,
		"path":    "/api/buckets",
		"remote":  req.RemoteAddr,
	} {
		if rec[field] != want {
			t.Errorf("access record %s = %v, want %v", field, rec[field], want)
		}
	}
	if rec["time"] == nil || rec["time"] == "" {
		t.Errorf("access record carries no time: %v", rec)
	}
}

func TestAccessAuditCarriesNoTokenMaterial(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets?token="+ownerToken, nil))
	buf := captureSlog(t)

	do(srv, req)

	if logged := buf.String(); strings.Contains(logged, ownerToken) {
		t.Fatalf("token material leaked into the log: %s", logged)
	}
}

func TestDeniedRequestEmitsNoAccessAudit(t *testing.T) {
	srv := newServer(fixedRegistry{})
	req := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
	req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
	buf := captureSlog(t)

	do(srv, req)

	if records := logRecordsNamed(buf, "overseer.auth.access"); len(records) != 0 {
		t.Fatalf("non-owner GET /api/buckets emitted %d access records, want 0", len(records))
	}
}

const (
	healthSeedRefresh = "seed-refresh-token-SECRET-health"
	healthAnonKey     = "anon-key-SECRET-health"
	healthPassword    = "readonly-PASSWORD-SECRET-health"
)

type ownerHealthBody struct {
	LastCycle     string `json:"lastCycle"`
	BucketsOK     int    `json:"bucketsOk"`
	BucketsFailed int    `json:"bucketsFailed"`
	Credential    *struct {
		OK                  bool   `json:"ok"`
		LastRefresh         string `json:"lastRefresh"`
		ConsecutiveFailures int    `json:"consecutiveFailures"`
		PersistFailed       bool   `json:"persistFailed"`
		PasswordGrant       bool   `json:"passwordGrant"`
		LastError           string `json:"lastError"`
	} `json:"credential"`
}

func rejectingTokenSource(t *testing.T) *goapi.RefreshingTokenSource {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"refresh_token_already_used"}`)
	}))
	t.Cleanup(endpoint.Close)
	src, err := goapi.NewRefreshingTokenSource(endpoint.URL, healthAnonKey, healthSeedRefresh,
		goapi.WithRefreshHTTPClient(endpoint.Client()),
		goapi.WithPasswordGrant("readonly@altune.test", healthPassword))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	return src
}

func getOwnerHealth(t *testing.T, srv http.Handler) (int, string, ownerHealthBody) {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/health", nil)))
	var body ownerHealthBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("owner health unmarshal: %v", err)
		}
	}
	return rec.Code, rec.Body.String(), body
}

func TestOwnerHealthRejectsMissingBearer(t *testing.T) {
	srv := newServer(fixedRegistry{})

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/health without bearer = %d, want 401", rec.Code)
	}
}

func TestOwnerHealthForbidsNonOwner(t *testing.T) {
	srv := newServer(fixedRegistry{})
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+nonOwnerToken)

	rec := do(srv, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/health with non-owner token = %d, want 403", rec.Code)
	}
}

func TestOwnerHealthShowsAFailingCredentialWithoutItsSecrets(t *testing.T) {
	tokens := rejectingTokenSource(t)
	_, _ = tokens.Token(context.Background())
	srv := newServer(fixedRegistry{}, shell.WithCredentialHealth(tokens.Health))

	code, raw, body := getOwnerHealth(t, srv)

	if code != http.StatusOK {
		t.Fatalf("owner GET /api/health = %d, want 200", code)
	}
	if body.Credential == nil {
		t.Fatalf("owner health %s carries no credential", raw)
	}
	if body.Credential.OK || body.Credential.ConsecutiveFailures < 1 {
		t.Errorf("credential = %+v, want ok=false with the failure count", *body.Credential)
	}
	if body.Credential.LastError == "" || !body.Credential.PasswordGrant {
		t.Errorf("credential = %+v, want the failure class and the configured password grant", *body.Credential)
	}
	for _, secret := range []string{healthSeedRefresh, healthAnonKey, healthPassword, "readonly@altune.test", "refresh_token_already_used"} {
		if strings.Contains(raw, secret) {
			t.Errorf("owner health %s leaks %q", raw, secret)
		}
	}
}

func TestOwnerHealthReportsTheLastCollectCycle(t *testing.T) {
	lastCycle := time.Date(2026, 9, 18, 10, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	srv := newServer(fixedRegistry{}, fixedCollectStatus(shell.CollectStatus{
		Healthy: true, LastCycle: lastCycle, OK: 6, Failed: 4,
	}))

	_, raw, body := getOwnerHealth(t, srv)

	if body.LastCycle != "2026-09-18T08:00:00Z" || body.BucketsOK != 6 || body.BucketsFailed != 4 {
		t.Errorf("owner health = %s, want the last cycle in UTC and its counts", raw)
	}
}

func TestOwnerHealthOmitsCredentialWhenNoRefreshingSourceIsWired(t *testing.T) {
	srv := newServer(fixedRegistry{})

	_, raw, body := getOwnerHealth(t, srv)

	if body.Credential != nil || strings.Contains(raw, "credential") {
		t.Errorf("owner health %s reports a credential nobody wired", raw)
	}
}

func TestPublicHealthStaysMinimalWithACredentialWired(t *testing.T) {
	tokens := rejectingTokenSource(t)
	_, _ = tokens.Token(context.Background())
	srv := newServer(fixedRegistry{},
		shell.WithCredentialHealth(tokens.Health),
		fixedCollectStatus(shell.CollectStatus{Healthy: true, LastCycle: time.Now(), OK: 1, Failed: 2}))

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil))

	var fields map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("health unmarshal: %v", err)
	}
	for _, key := range []string{"status", "last_cycle", "buckets_ok", "buckets_failed"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("public /health %s lost %q", rec.Body.String(), key)
		}
	}
	if len(fields) != 4 {
		t.Errorf("public /health %s, want exactly status, last_cycle, buckets_ok, buckets_failed", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Errorf("public /health = %d with a failing credential, want 200", rec.Code)
	}
}

var dataRoutes = []string{"/api/buckets", "/api/buckets/reliability/series", "/api/stream"}

type fakeSeries struct {
	points  map[string][]core.Point
	names   []string
	failure error
	from    time.Time
	to      time.Time
}

func (f *fakeSeries) Names(string) ([]string, error) { return f.names, f.failure }

func (f *fakeSeries) Query(_, series string, from, to time.Time) ([]core.Point, error) {
	f.from, f.to = from, to
	return f.points[series], nil
}

func reliabilityRegistry() fixedRegistry {
	return fixedRegistry{buckets: []core.Bucket{stubBucket{id: "reliability", state: core.StateLive}}}
}

type seriesBody struct {
	Bucket string `json:"bucket"`
	Range  string `json:"range"`
	Series map[string][]struct {
		At string  `json:"at"`
		V  float64 `json:"v"`
	} `json:"series"`
}

func getSeries(t *testing.T, srv http.Handler, path string) (*httptest.ResponseRecorder, seriesBody) {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, path, nil)))
	var body seriesBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v (%s)", path, err, rec.Body.String())
		}
	}
	return rec, body
}

func TestEveryDataRouteRejectsAMissingBearer(t *testing.T) {
	srv := newServer(reliabilityRegistry())
	for _, path := range dataRoutes {
		if rec := do(srv, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without bearer = %d, want 401", path, rec.Code)
		}
	}
}

func TestEveryDataRouteForbidsANonOwner(t *testing.T) {
	srv := newServer(reliabilityRegistry())
	for _, path := range dataRoutes {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		if rec := do(srv, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s with non-owner token = %d, want 403", path, rec.Code)
		}
	}
}

func TestGuardedHealthDataRejectsMissingBearerAndNonOwner(t *testing.T) {
	t.Skip("lands in [Task]: overseer exposes credential and cycle health to the owner (#2359)")
}

func TestSeriesReturnsEveryNamedSeriesForTheWindow(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	source := &fakeSeries{
		names: []string{"latency_ms", "up"},
		points: map[string][]core.Point{
			"up":         {{At: at, Value: 1}},
			"latency_ms": {{At: at, Value: 38.5}},
		},
	}
	srv := newServer(reliabilityRegistry(), shell.WithSeries(source))

	rec, body := getSeries(t, srv, "/api/buckets/reliability/series?range=24h")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if body.Bucket != "reliability" || body.Range != "24h" {
		t.Fatalf("envelope = %s/%s, want reliability/24h", body.Bucket, body.Range)
	}
	if got := body.Series["up"]; len(got) != 1 || got[0].V != 1 || got[0].At != "2026-09-01T12:00:00Z" {
		t.Fatalf("up series = %+v, want one point {2026-09-01T12:00:00Z 1}", got)
	}
	if got := body.Series["latency_ms"]; len(got) != 1 || got[0].V != 38.5 {
		t.Fatalf("latency_ms series = %+v, want one point of 38.5", got)
	}
	if window := source.to.Sub(source.from); window != 24*time.Hour {
		t.Fatalf("queried window = %v, want 24h", window)
	}
}

func TestSeriesRangeDefaultsToOneHour(t *testing.T) {
	source := &fakeSeries{names: []string{"up"}}
	srv := newServer(reliabilityRegistry(), shell.WithSeries(source))

	rec, body := getSeries(t, srv, "/api/buckets/reliability/series")

	if rec.Code != http.StatusOK || body.Range != "1h" {
		t.Fatalf("status %d range %q, want 200 1h", rec.Code, body.Range)
	}
	if window := source.to.Sub(source.from); window != time.Hour {
		t.Fatalf("queried window = %v, want 1h", window)
	}
	if got, present := body.Series["up"]; !present || got == nil {
		t.Fatalf("an empty series must serialize as [], got %s", rec.Body.String())
	}
}

func TestSeriesRejectsAnUnknownRange(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&fakeSeries{}))
	for _, query := range []string{"range=2h", "range=1H", "range=7d;drop", "range=30d", "range=", "range=1h&range=7d", "range=%zz"} {
		rec, _ := getSeries(t, srv, "/api/buckets/reliability/series?"+query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("?%s = %d, want 400", query, rec.Code)
		}
	}
}

func TestSeriesUnknownBucketIsNotFound(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&fakeSeries{names: []string{"up"}}))
	rec, _ := getSeries(t, srv, "/api/buckets/nope/series")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown bucket = %d, want 404", rec.Code)
	}
}

func TestSeriesWithNoHistoryIsAnEmptySeriesMap(t *testing.T) {
	srv := newServer(reliabilityRegistry())
	rec, _ := getSeries(t, srv, "/api/buckets/reliability/series")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"bucket":"reliability","range":"1h","series":{}}` {
		t.Fatalf("body = %s, want an empty series map", got)
	}
}

func TestSeriesReadFailureIsUnavailableAndLogged(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&fakeSeries{failure: errors.New("disk I/O error")}))
	logs := captureSlog(t)

	rec, _ := getSeries(t, srv, "/api/buckets/reliability/series")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if len(logRecordsNamed(logs, "overseer.shell.series_read_failed")) != 1 {
		t.Fatalf("read failure not logged: %s", logs.String())
	}
}

type minuteSeries struct {
	fakeSeries
	minutes map[string][]core.Minute
}

func (m *minuteSeries) Minutes(_, series string, from, to time.Time) ([]core.Minute, error) {
	m.from, m.to = from, to
	return m.minutes[series], nil
}

type wirePoint struct {
	At  string   `json:"at"`
	V   float64  `json:"v"`
	Min *float64 `json:"min"`
	Max *float64 `json:"max"`
}

func getWireSeries(t *testing.T, srv http.Handler, path string) map[string][]wirePoint {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, path, nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200 (%s)", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Series map[string][]wirePoint `json:"series"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return body.Series
}

func minuteFixture() *minuteSeries {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return &minuteSeries{
		fakeSeries: fakeSeries{
			names:  []string{"latency_ms"},
			points: map[string][]core.Point{"latency_ms": {{At: at, Value: 38.5}}},
		},
		minutes: map[string][]core.Minute{"latency_ms": {{At: at, Min: 10, Max: 90, Avg: 40}}},
	}
}

func TestSevenDaySeriesServesMinuteAveragesWithMinAndMax(t *testing.T) {
	source := minuteFixture()
	srv := newServer(reliabilityRegistry(), shell.WithSeries(source))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=7d")

	got := series["latency_ms"]
	if len(got) != 1 || got[0].V != 40 || got[0].Min == nil || *got[0].Min != 10 || got[0].Max == nil || *got[0].Max != 90 {
		t.Fatalf("7d latency_ms = %+v, want one minute {v 40 min 10 max 90}", got)
	}
	if window := source.to.Sub(source.from); window != 7*24*time.Hour {
		t.Fatalf("queried window = %v, want 7d", window)
	}
}

func TestDaySeriesServesRawPointsWithoutMinOrMax(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(minuteFixture()))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=24h")

	got := series["latency_ms"]
	if len(got) != 1 || got[0].V != 38.5 || got[0].Min != nil || got[0].Max != nil {
		t.Fatalf("24h latency_ms = %+v, want the raw point with no min or max", got)
	}
}

func TestSevenDaySeriesFromAReaderWithoutMinutesServesItsPoints(t *testing.T) {
	source := minuteFixture()
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&source.fakeSeries))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=7d")

	if got := series["latency_ms"]; len(got) != 1 || got[0].V != 38.5 || got[0].Min != nil {
		t.Fatalf("7d latency_ms from a points-only reader = %+v, want its one point", got)
	}
}

func TestSevenDaySeriesFromTheHistoryStoreCarriesMinAndMax(t *testing.T) {
	store := history.Open(filepath.Join(t.TempDir(), "history.db"), history.WithSchemaTimeout(time.Minute))
	t.Cleanup(func() { _ = store.Close() })
	minute := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	for i, value := range []float64{20, 60, 40} {
		store.Record("reliability", "latency_ms", core.Point{At: minute.Add(time.Duration(i) * 10 * time.Second), Value: value})
	}
	srv := newServer(reliabilityRegistry(), shell.WithSeries(store))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=7d")

	got := series["latency_ms"]
	if len(got) != 1 || got[0].V != 40 || got[0].Min == nil || *got[0].Min != 20 || got[0].Max == nil || *got[0].Max != 60 {
		t.Fatalf("7d latency_ms from the store = %+v, want one minute {v 40 min 20 max 60}", got)
	}
}

type keyedStubBucket struct {
	stubBucket
	key string
}

func (s keyedStubBucket) KeySeries() string { return s.key }

type erroringSeries struct{}

func (erroringSeries) Names(string) ([]string, error) { return nil, nil }

func (erroringSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, context.DeadlineExceeded
}

type bucketsBody struct {
	Buckets []struct {
		ID    string `json:"id"`
		Spark []struct {
			At string  `json:"at"`
			V  float64 `json:"v"`
		} `json:"spark"`
	} `json:"buckets"`
}

func getBuckets(t *testing.T, srv http.Handler) (*httptest.ResponseRecorder, bucketsBody) {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	var body bucketsBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /api/buckets: %v (%s)", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestBucketsSparkHoldsAtMostThirtyPointsFromTheTail(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	points := make([]core.Point, 0, 40)
	for i := 0; i < 40; i++ {
		points = append(points, core.Point{At: at.Add(time.Duration(i) * time.Second), Value: float64(i)})
	}
	source := &fakeSeries{points: map[string][]core.Point{"latency_ms": points}}
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(source))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(body.Buckets))
	}
	spark := body.Buckets[0].Spark
	if len(spark) != 30 {
		t.Fatalf("spark holds %d points, want 30", len(spark))
	}
	if spark[0].V != 10 {
		t.Fatalf("spark[0].V = %v, want 10 (the tail of the last 30 of 40)", spark[0].V)
	}
	if spark[29].V != 39 {
		t.Fatalf("spark[29].V = %v, want 39 (the most recent point)", spark[29].V)
	}
}

func TestBucketsOmitSparkWhenTheBucketHasNoKeySeries(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{stubBucket{id: "cost", state: core.StateLive}}}
	source := &fakeSeries{points: map[string][]core.Point{"anything": {{At: time.Now(), Value: 1}}}}
	srv := newServer(reg, shell.WithSeries(source))

	rec, _ := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); jsonHasKey(got, "spark") {
		t.Fatalf("body contains a spark field for a bucket with no KeySeries: %s", got)
	}
}

func jsonHasKey(body, key string) bool {
	var envelope struct {
		Buckets []map[string]json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return false
	}
	for _, bucket := range envelope.Buckets {
		if _, ok := bucket[key]; ok {
			return true
		}
	}
	return false
}

func TestBucketsOmitSparkWhenTheHistoryReadFails(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(erroringSeries{}))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("a history read failure must degrade the spark, not the whole response; status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 1 || body.Buckets[0].Spark != nil {
		t.Fatalf("spark = %+v, want none when the history read fails", body.Buckets)
	}
}

type panicKeySeriesBucket struct{ stubBucket }

func (panicKeySeriesBucket) KeySeries() string { panic("keyseries blew up") }

type panicSeries struct{}

func (panicSeries) Names(string) ([]string, error) { return nil, nil }

func (panicSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	panic("query blew up")
}

func TestBucketsSurviveAPanickingKeySeries(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		panicKeySeriesBucket{stubBucket: stubBucket{id: "boom", state: core.StateLive}},
		stubBucket{id: "ok", state: core.StateLive},
	}}
	srv := newServer(reg)

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("a panicking KeySeries must degrade one bucket's spark, not the response; status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2 despite the panic", len(body.Buckets))
	}
}

func TestBucketsSurviveAPanickingHistoryRead(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(panicSeries{}))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("a panicking history read must degrade the spark, not the response; status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 1 || body.Buckets[0].Spark != nil {
		t.Fatalf("spark = %+v, want none when the history read panics", body.Buckets)
	}
}

func TestStreamSurvivesAPanickingKeySeries(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		panicKeySeriesBucket{stubBucket: stubBucket{id: "boom", state: core.StateLive}},
		stubBucket{id: "ok", state: core.StateLive},
	}}
	srv := newServer(reg, shell.WithStreamInterval(5*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/stream", nil)).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stream status = %d, want 200 despite a panicking KeySeries", rec.Code)
	}
	if got := rec.Body.String(); got == "" {
		t.Fatalf("stream body is empty, want the initial frame despite the panic")
	}
}

type recordingTailReader struct {
	calls int
	limit int
}

func (r *recordingTailReader) Names(string) ([]string, error) { return nil, nil }

func (r *recordingTailReader) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	r.calls++
	return []core.Point{{At: time.Now(), Value: -1}}, nil
}

func (r *recordingTailReader) Tail(_, _ string, _, _ time.Time, limit int) ([]core.Point, error) {
	r.limit = limit
	return []core.Point{{At: time.Now(), Value: 1}}, nil
}

func TestBucketsSparkPrefersTheTailReaderPushDown(t *testing.T) {
	reader := &recordingTailReader{}
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(reader))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if reader.calls != 0 {
		t.Fatalf("Query called %d times, want 0: TailReader should be preferred", reader.calls)
	}
	if reader.limit != 30 {
		t.Fatalf("Tail limit = %d, want 30", reader.limit)
	}
	if spark := body.Buckets[0].Spark; len(spark) != 1 || spark[0].V != 1 {
		t.Fatalf("spark = %+v, want the Tail result", spark)
	}
}
