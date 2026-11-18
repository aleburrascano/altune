package app_test

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
