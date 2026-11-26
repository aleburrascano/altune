package app_test

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const (
	serveWait   = 5 * time.Second
	stopWait    = 10 * time.Second
	settleWait  = 20 * time.Second
	ownerUserID = "owner-sub"
	jwtSecret   = "app-test-hs256-secret"
)

var httpClient = &http.Client{Timeout: 2 * time.Second}

func testConfig(historyPath string) *config.Config {
	return &config.Config{
		Host:                 "127.0.0.1",
		OwnerUserID:          ownerUserID,
		SupabaseURL:          "https://x.supabase.co",
		SupabaseJWTSecret:    jwtSecret,
		TickInterval:         time.Hour,
		BucketTimeout:        time.Second,
		HistoryPath:          historyPath,
		HistorySchemaTimeout: time.Minute,
	}
}

func newConfig(t *testing.T) *config.Config {
	t.Helper()
	return testConfig(filepath.Join(t.TempDir(), "history.db"))
}

type running struct {
	cfg    *config.Config
	base   string
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}

func launch(cfg *config.Config, buckets []core.Bucket) (*running, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	cfg.Host, cfg.Port = "127.0.0.1", port
	a := app.New(cfg, buckets, os.Getenv)
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{cfg: cfg, base: fmt.Sprintf("http://127.0.0.1:%d", port), cancel: cancel, done: make(chan struct{})}
	go func() {
		r.err = a.Run(ctx)
		close(r.done)
	}()
	return r, r.awaitServing()
}

func (r *running) awaitServing() error {
	deadline := time.Now().Add(serveWait)
	for time.Now().Before(deadline) {
		if _, _, err := r.get("/health", ""); err == nil {
			return nil
		}
		select {
		case <-r.done:
			return fmt.Errorf("Run returned before serving: %w", r.err)
		case <-time.After(5 * time.Millisecond):
		}
	}
	r.cancel()
	return fmt.Errorf("the app did not answer /health within %v", serveWait)
}

func (r *running) get(path, token string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, r.base+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, errors.Join(err, resp.Body.Close())
}

func (r *running) stop(within time.Duration) error {
	r.cancel()
	select {
	case <-r.done:
		return r.err
	case <-time.After(within):
		return fmt.Errorf("Run did not return within %v of its context being cancelled", within)
	}
}

func boot(t *testing.T, cfg *config.Config, buckets ...core.Bucket) *running {
	t.Helper()
	r, err := launch(cfg, buckets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.stop(stopWait) })
	return r
}

func (r *running) halt(t *testing.T) {
	t.Helper()
	if err := r.stop(stopWait); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

type healthBody struct {
	Status        string `json:"status"`
	LastCycle     string `json:"last_cycle"`
	BucketsOK     int    `json:"buckets_ok"`
	BucketsFailed int    `json:"buckets_failed"`
}

func (r *running) health(t *testing.T) (int, healthBody) {
	t.Helper()
	code, body, err := r.get("/health", "")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	var h healthBody
	if err := json.Unmarshal(body, &h); err != nil {
		t.Fatalf("GET /health body %q: %v", body, err)
	}
	return code, h
}

func ownerToken(cfg *config.Config) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   cfg.OwnerUserID,
		Issuer:    cfg.IssuerURL(),
		Audience:  jwt.ClaimStrings{"authenticated"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.SupabaseJWTSecret))
}

func (r *running) ownerGet(t *testing.T, path string) []byte {
	t.Helper()
	token, err := ownerToken(r.cfg)
	if err != nil {
		t.Fatalf("sign owner token: %v", err)
	}
	code, body, err := r.get(path, token)
	if err != nil || code != http.StatusOK {
		t.Fatalf("GET %s as the owner = %d %q, %v; want 200", path, code, body, err)
	}
	return body
}

type seriesPoint struct {
	At time.Time `json:"at"`
	V  float64   `json:"v"`
}

func (r *running) series(t *testing.T, bucket string) map[string][]seriesPoint {
	t.Helper()
	body := r.ownerGet(t, "/api/buckets/"+bucket+"/series")
	var resp struct {
		Series map[string][]seriesPoint `json:"series"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("series body %q: %v", body, err)
	}
	return resp.Series
}

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *logBuffer) named(msg string) []map[string]any {
	var matched []map[string]any
	for _, line := range bytes.Split([]byte(l.String()), []byte("\n")) {
		var rec map[string]any
		if json.Unmarshal(line, &rec) == nil && rec["msg"] == msg {
			matched = append(matched, rec)
		}
	}
	return matched
}

func captureLog(t *testing.T) *logBuffer {
	t.Helper()
	logs := &logBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(settleWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(time.Millisecond)
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

type fakeBucket struct {
	id       string
	collect  func(ctx context.Context, call int32) ([]core.Signal, error)
	store    func([]core.Signal)
	collects atomic.Int32
	stores   atomic.Int32
}

func plain(id string) *fakeBucket { return &fakeBucket{id: id} }

func (b *fakeBucket) Meta() core.Meta { return core.Meta{ID: b.id} }

func (b *fakeBucket) Collect(ctx context.Context) ([]core.Signal, error) {
	call := b.collects.Add(1)
	if b.collect == nil {
		return []core.Signal{{Text: "ok"}}, nil
	}
	return b.collect(ctx, call)
}

func (b *fakeBucket) Store(signals []core.Signal) {
	b.stores.Add(1)
	if b.store != nil {
		b.store(signals)
	}
}

func (*fakeBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

func (b *fakeBucket) collectedAtLeast(n int32) func() bool {
	return func() bool { return b.collects.Load() >= n }
}

func failing(id string) *fakeBucket {
	return &fakeBucket{id: id, collect: func(context.Context, int32) ([]core.Signal, error) {
		return nil, errors.New("source down")
	}}
}

func panicking(id, where string) *fakeBucket {
	b := &fakeBucket{id: id}
	if where == "collect" {
		b.collect = func(context.Context, int32) ([]core.Signal, error) { panic("collect blew up") }
	} else {
		b.store = func([]core.Signal) { panic("store blew up") }
	}
	return b
}

type metaPanicsBucket struct {
	*fakeBucket
	armed atomic.Bool
}

func (b *metaPanicsBucket) Meta() core.Meta {
	if b.armed.Load() {
		panic("meta boom")
	}
	return b.fakeBucket.Meta()
}

func TestAPanicInACollectCycleIsLoggedAtOnceAndCollectionGoesOn(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	bucket := &metaPanicsBucket{fakeBucket: plain("fragile")}
	r := boot(t, cfg, bucket)
	eventually(t, bucket.collectedAtLeast(1), "the bucket never collected before the panic")

	bucket.armed.Store(true)
	eventually(t, func() bool { return len(logs.named("overseer.collect.tick_loop_panicked")) > 0 }, "a panicking cycle was not logged while the overseer kept running")
	bucket.armed.Store(false)
	before := bucket.collects.Load()
	eventually(t, bucket.collectedAtLeast(before+2), "the tick loop stopped collecting after a panicking cycle")

	r.halt(t)
}

func wantCounts(t *testing.T, r *running, ok, failed int) {
	t.Helper()
	code, h := r.health(t)
	if code != http.StatusOK || h.Status != "ok" {
		t.Errorf("GET /health = %d %q; want 200 ok", code, h.Status)
	}
	if h.BucketsOK != ok || h.BucketsFailed != failed {
		t.Errorf("GET /health counts = ok:%d failed:%d; want ok:%d failed:%d", h.BucketsOK, h.BucketsFailed, ok, failed)
	}
}

func wantCycleLine(t *testing.T, logs *logBuffer, ok, failed int) {
	t.Helper()
	cycles := logs.named("overseer.collect.cycle")
	if len(cycles) == 0 {
		t.Fatal("the app logged no overseer.collect.cycle heartbeat")
	}
	if cycles[0]["ok"] != float64(ok) || cycles[0]["failed"] != float64(failed) {
		t.Errorf("heartbeat = ok:%v failed:%v; want ok:%d failed:%d", cycles[0]["ok"], cycles[0]["failed"], ok, failed)
	}
}

func TestACycleCountsItsHealthyAndFailedBuckets(t *testing.T) {
	logs := captureLog(t)

	r := boot(t, newConfig(t), plain("healthy"), failing("down"))

	wantCounts(t, r, 1, 1)
	r.halt(t)
	wantCycleLine(t, logs, 1, 1)
}

func TestHealthIsOkWithItsCountsRightAfterTheFirstCycle(t *testing.T) {
	captureLog(t)

	r := boot(t, newConfig(t), plain("healthy"))

	wantCounts(t, r, 1, 0)
	if _, h := r.health(t); h.LastCycle == "" {
		t.Error("GET /health reports no last cycle after one completed")
	}
}

func TestAStorePanicCountsAsAFailedBucketAndSparesItsSibling(t *testing.T) {
	logs := captureLog(t)

	r := boot(t, newConfig(t), panicking("boom", "store"), plain("healthy"))

	wantCounts(t, r, 1, 1)
	r.halt(t)
	wantCycleLine(t, logs, 1, 1)
}

func TestACollectPanicIsContainedAndLoggedOnceAtError(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	boom := panicking("boom", "collect")

	r := boot(t, cfg, boom)
	eventually(t, boom.collectedAtLeast(4), "the panicking bucket was not collected on later ticks; the loop died with it")
	wantCounts(t, r, 0, 1)
	r.halt(t)

	panics := logs.named("overseer.collect.bucket_panic")
	if len(panics) != 1 {
		t.Fatalf("a panicking bucket logged bucket_panic %d times over 3 cycles; want 1", len(panics))
	}
	if panics[0]["level"] != "ERROR" || panics[0]["stage"] != "Collect" {
		t.Errorf("bucket_panic = level %v stage %v; want ERROR Collect", panics[0]["level"], panics[0]["stage"])
	}
	if got := len(logs.named("overseer.collect.source_down")); got != 0 {
		t.Errorf("a panic was re-logged as source_down %d times; a crash must not be downgraded to WARN", got)
	}
	if got := boom.stores.Load(); got != 0 {
		t.Errorf("the panicking bucket was handed signals to Store %d times; a contained panic yields none", got)
	}
}

func TestASustainedOutageLogsOneTransitionNotAFloodPerTick(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	var down atomic.Bool
	down.Store(true)
	goapi := &fakeBucket{id: "goapi", collect: func(context.Context, int32) ([]core.Signal, error) {
		if down.Load() {
			return nil, errors.New("source down")
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}

	r := boot(t, cfg, goapi)
	eventually(t, goapi.collectedAtLeast(6), "the outage did not last 5 cycles")
	down.Store(false)
	eventually(t, goapi.collectedAtLeast(goapi.collects.Load()+2), "no cycle completed after the source recovered")
	r.halt(t)

	if got := len(logs.named("overseer.collect.source_down")); got != 1 {
		t.Errorf("a multi-cycle outage logged source_down %d times; want 1 transition, not a per-tick flood", got)
	}
	if got := len(logs.named("overseer.collect.source_up")); got != 1 {
		t.Errorf("recovery logged source_up %d times; want exactly 1", got)
	}
}

func TestAStalledBucketIsCutOffAtItsDeadlineAndItsSiblingStillStores(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.BucketTimeout = 50 * time.Millisecond
	cancelled := make(chan struct{})
	stalled := &fakeBucket{id: "a-stalled", collect: func(ctx context.Context, _ int32) ([]core.Signal, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}
	healthy := plain("z-healthy")

	r := boot(t, cfg, stalled, healthy)

	if !isClosed(cancelled) {
		t.Error("the stalled bucket's Collect was not cancelled; the per-bucket deadline did not fire")
	}
	if healthy.stores.Load() != 1 {
		t.Error("the bucket queued behind the stalled one never stored; the cycle was starved")
	}
	wantCounts(t, r, 1, 1)
	r.halt(t)
	wantCycleLine(t, logs, 1, 1)
}

func TestHealthGoesStalledWhenTheCollectLoopWedges(t *testing.T) {
	captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 10 * time.Millisecond
	cfg.BucketTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	wedged := &fakeBucket{id: "wedged", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call > 1 {
			<-release
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}

	r := boot(t, cfg, wedged)
	t.Cleanup(releaseOnce)
	eventually(t, func() bool {
		code, h := r.health(t)
		return code == http.StatusServiceUnavailable && h.Status == "collect_stalled"
	}, "GET /health stayed green on a wedged collect loop well past its staleness budget")

	releaseOnce()
	r.halt(t)
}

func TestStartedBackgroundWorkKeepsRunningUntilRunStops(t *testing.T) {
	captureLog(t)
	scheduler := &startedBucket{fakeBucket: plain("scheduler"), stopped: make(chan struct{})}

	r := boot(t, newConfig(t), scheduler)
	eventually(t, func() bool { return scheduler.refreshes.Load() >= 2 },
		"background work fired fewer than 2 refreshes through the Start hook; it froze after one, the #1812 regression")
	r.halt(t)

	select {
	case <-scheduler.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("background work did not drain within 2s of Run returning; the goroutine leaked past shutdown")
	}
}

func TestABucketWithoutAStartHookStillCollectsEveryTick(t *testing.T) {
	captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	b := plain("plain")

	r := boot(t, cfg, b)

	eventually(t, b.collectedAtLeast(3), "a bucket with no Start hook stopped being collected after start-up")
	wantCounts(t, r, 1, 0)
}

type startedBucket struct {
	*fakeBucket
	refreshes atomic.Int32
	stopped   chan struct{}
}

func (s *startedBucket) Start(ctx context.Context) {
	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshes.Add(1)
			}
		}
	}()
}

var ownerHealthDir = flag.String("owner-health-dir", "", "serve one app, write its GET /api/health body into this dir and exit")

func TestMain(m *testing.M) {
	flag.Parse()
	if *ownerHealthDir == "" {
		os.Exit(m.Run())
	}
	if err := writeOwnerHealth(*ownerHealthDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func writeOwnerHealth(dir string) error {
	cfg := testConfig(filepath.Join(dir, "history.db"))
	r, err := launch(cfg, nil)
	if err != nil {
		return err
	}
	token, err := ownerToken(cfg)
	if err != nil {
		return errors.Join(err, r.stop(stopWait))
	}
	code, body, err := r.get("/api/health", token)
	if err = errors.Join(err, r.stop(stopWait)); err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET /api/health as the owner = %d %q; want 200", code, body)
	}
	return os.WriteFile(filepath.Join(dir, "owner-health.json"), body, 0o600)
}

type ownerHealth struct {
	Credential *struct {
		PasswordGrant bool `json:"passwordGrant"`
	} `json:"credential"`
}

func ownerHealthInAFreshProcess(t *testing.T, mode string, env ...string) ownerHealth {
	t.Helper()
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-owner-health-dir="+dir)
	child.Env = append(withoutOverseerEnv(os.Environ()), env...)
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the app in a fresh process failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "mode="+mode) {
		t.Fatalf("the fresh process did not select the %s token source:\n%s", mode, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "owner-health.json"))
	if err != nil {
		t.Fatalf("read the child's /api/health body: %v", err)
	}
	var h ownerHealth
	if err := json.Unmarshal(body, &h); err != nil {
		t.Fatalf("GET /api/health body %q: %v", body, err)
	}
	return h
}

func withoutOverseerEnv(environ []string) []string {
	var kept []string
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "OVERSEER_") {
			kept = append(kept, kv)
		}
	}
	return kept
}

func TestOwnerHealthReportsARefreshingCredentialsOwnState(t *testing.T) {
	h := ownerHealthInAFreshProcess(t, "refreshing",
		"OVERSEER_SUPABASE_URL=https://x.supabase.co",
		"OVERSEER_SUPABASE_ANON_KEY=anon-key",
		"OVERSEER_GOAPI_READONLY_REFRESH_TOKEN=seed-refresh",
		"OVERSEER_GOAPI_READONLY_EMAIL=readonly@altune.test",
		"OVERSEER_GOAPI_READONLY_PASSWORD=password",
		"OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE="+filepath.Join(t.TempDir(), "refresh_token"),
	)

	if h.Credential == nil {
		t.Fatal("a refreshing token source wired no credential health into GET /api/health")
	}
	if !h.Credential.PasswordGrant {
		t.Error("the credential health on GET /api/health is not the refreshing source's own state")
	}
}

func TestOwnerHealthOmitsCredentialsForAStaticToken(t *testing.T) {
	h := ownerHealthInAFreshProcess(t, "static", "OVERSEER_GOAPI_READONLY_TOKEN=static-jwt")

	if h.Credential != nil {
		t.Error("a static token has no refresh state to report, yet GET /api/health carries credential health")
	}
}

type seriesBucket struct {
	*fakeBucket
	panics bool
}

func (s seriesBucket) UseSeries(series core.Series) {
	if s.panics {
		panic("UseSeries blew up")
	}
	series.Record(s.id, "up", core.Point{At: time.Now(), Value: 1})
}

func wantOnePoint(t *testing.T, r *running, bucket string) {
	t.Helper()
	if got := r.series(t, bucket)["up"]; len(got) != 1 || got[0].V != 1 {
		t.Fatalf("history series %s/up = %+v; want the one point the bucket recorded through the series it was handed", bucket, got)
	}
}

func TestACorruptHistoryFileStillBootsAndSaysSo(t *testing.T) {
	cfg := newConfig(t)
	if err := os.WriteFile(cfg.HistoryPath, []byte(strings.Repeat("garbage, not sqlite ", 300)), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	logs := captureLog(t)

	r := boot(t, cfg, plain("reliability"))

	if code, _ := r.health(t); code != http.StatusOK {
		t.Errorf("GET /health over a corrupt history file = %d; want 200", code)
	}
	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Errorf("log does not say history is unavailable: %s", logs.String())
	}
	if got := r.series(t, "reliability"); len(got) != 0 {
		t.Errorf("history over a corrupt file lists %v; want empty", got)
	}
}

func TestEverySeriesWriterIsHandedTheHistoryStore(t *testing.T) {
	captureLog(t)

	r := boot(t, newConfig(t), seriesBucket{fakeBucket: plain("writer")}, plain("plain"))

	wantOnePoint(t, r, "writer")
}

func TestAPanickingSeriesWriterDoesNotStopTheNextOneGettingTheStore(t *testing.T) {
	logs := captureLog(t)

	r := boot(t, newConfig(t), seriesBucket{fakeBucket: plain("a-boom"), panics: true}, seriesBucket{fakeBucket: plain("b-writer")})

	wantOnePoint(t, r, "b-writer")
	panics := logs.named("overseer.start.bucket_panic")
	if len(panics) != 1 || panics[0]["stage"] != "UseSeries" || panics[0]["bucket"] != "a-boom" {
		t.Errorf("start bucket_panic lines = %v; want one for a-boom at stage UseSeries", panics)
	}
}

func TestRunReturnsNilSoonAfterItsContextIsCancelled(t *testing.T) {
	captureLog(t)
	r := boot(t, newConfig(t), plain("plain"))

	if err := r.stop(2 * time.Second); err != nil {
		t.Fatalf("Run after cancel: %v; the history pruner or store outlived shutdown", err)
	}
}

type lateWriterBucket struct {
	*fakeBucket
	series  core.Series
	running sync.WaitGroup
}

func (b *lateWriterBucket) UseSeries(series core.Series) { b.series = series }

func (b *lateWriterBucket) Start(ctx context.Context) {
	b.running.Go(func() {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		b.series.Record(b.id, "late", core.Point{At: time.Now(), Value: 1})
	})
}

func (b *lateWriterBucket) Wait() { b.running.Wait() }

func TestRunJoinsStartedBucketsBeforeClosingHistory(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	since := time.Now().Add(-time.Minute)

	boot(t, cfg, &lateWriterBucket{fakeBucket: plain("late")}).halt(t)

	store := history.Open(cfg.HistoryPath, history.WithSchemaTimeout(time.Minute))
	t.Cleanup(func() { _ = store.Close() })
	points, err := store.Query("late", "late", since, time.Now().Add(time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("the started bucket's shutdown write = %+v, %v; want 1 point stored before history closed", points, err)
	}
	if failed := logs.named("history.write_failed"); len(failed) != 0 {
		t.Fatalf("a write reached the history store after it closed: %v", failed)
	}
}

const shutdownBudget = 50 * time.Millisecond

func TestRunReturnsWithinTheShutdownBudgetWhenTheTickLoopIgnoresCtxAndBlocks(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	cfg.BucketTimeout = time.Hour
	cfg.ShutdownBudget = shutdownBudget
	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	wedged := &fakeBucket{id: "wedged", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call == 2 {
			close(entered)
			<-release
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}
	r := boot(t, cfg, wedged)
	select {
	case <-entered:
	case <-time.After(settleWait):
		t.Fatal("the wedged bucket never entered its second, ctx-ignoring Collect call")
	}

	if err := r.stop(shutdownBudget + 5*time.Second); err != nil {
		t.Fatalf("Run with a wedged tick loop: %v", err)
	}

	warnings := logs.named("overseer.shutdown.tick_loop_did_not_stop")
	if len(warnings) != 1 {
		t.Fatalf("Run gave up on the wedged tick loop with %d tick_loop_did_not_stop warnings; want 1", len(warnings))
	}
	if got := warnings[0]["budget"]; got != float64(shutdownBudget) {
		t.Errorf("tick_loop_did_not_stop budget = %v, want the configured %d", got, shutdownBudget)
	}
}

func TestAnUnsetBudgetWaitsForAWedgedTickLoopInsteadOfGivingUpAtOnce(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	cfg.BucketTimeout = time.Hour
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	wedged := &fakeBucket{id: "wedged", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call == 2 {
			close(entered)
			<-release
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}
	r := boot(t, cfg, wedged)
	t.Cleanup(releaseOnce)
	select {
	case <-entered:
	case <-time.After(settleWait):
		t.Fatal("the wedged bucket never entered its second Collect call")
	}

	r.cancel()
	select {
	case <-r.done:
		t.Fatalf("Run returned (%v) at once while its tick loop was wedged; an unset ShutdownBudget must keep the 15s default, not mean zero", r.err)
	case <-time.After(200 * time.Millisecond):
	}
	releaseOnce()
	r.halt(t)

	if got := len(logs.named("overseer.shutdown.tick_loop_did_not_stop")); got != 0 {
		t.Errorf("a tick loop released within the default budget logged %d tick_loop_did_not_stop warnings; want none", got)
	}
}
