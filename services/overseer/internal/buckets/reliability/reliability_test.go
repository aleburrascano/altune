package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/shell"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "owner-token-owner-token-owner-tok" // >= 32 chars for the shell guard

// fakeReader is a controllable stand-in for the admin-read (mirror) path. A test
// sets the health snapshot and/or error it returns, exercising the mirror and
// its degrade-to-stale path with no network.
type fakeReader struct {
	mu     sync.Mutex
	health goapi.OperatorHealth
	err    error
}

func (f *fakeReader) AdminHealth(context.Context) (goapi.OperatorHealth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.health, f.err
}

func (f *fakeReader) set(h goapi.OperatorHealth, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.health, f.err = h, err
}

// fakeChecker is a controllable stand-in for the independent reachability poll.
// It shares no field with fakeReader, which is the whole point: the test can
// drive the poll signal to any value regardless of what the admin read reports.
type fakeChecker struct {
	mu     sync.Mutex
	health goapi.Health
	err    error
}

func (f *fakeChecker) Health(context.Context) (goapi.Health, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.health, f.err
}

func (f *fakeChecker) set(h goapi.Health, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.health, f.err = h, err
}

func healthyHealth() goapi.OperatorHealth {
	return goapi.OperatorHealth{
		DB:    "ok",
		Redis: "ok",
		Auth:  "ok",
		Detail: goapi.HealthDetail{
			CheckedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	}
}

func srcDown(op string) error {
	return &goapi.SourceDownError{Op: op, Err: errors.New("dial refused")}
}

// collectStore runs one collect/store cycle, the pair the shell drives on a tick.
func collectStore(t *testing.T, b *Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

// TestMirrorCollectStoreRender is the core Done proof: a healthy operator-health
// read flows into the mirror and renders as DB/Redis/Auth pills plus a bounded
// history sample.
func TestMirrorCollectStoreRender(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)

	if body := string(b.Render().Body); !strings.Contains(body, "no dependency health mirrored yet") {
		t.Errorf("empty render = %q, want a no-mirror gap", body)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while healthy = %v, want nil", err)
	}
	// Drive the independent poll so the reachability line has a signal.
	b.poller.pollOnce(context.Background())

	body := string(b.Render().Body)
	for _, want := range []string{"REACHABLE", "Dependency health", "DB: ok", "Redis: ok", "Auth: ok", "1 health sample(s)"} {
		if !strings.Contains(body, want) {
			t.Errorf("render missing %q\n---\n%s", want, body)
		}
	}
	if b.Meta().ID != "reliability" {
		t.Errorf("Meta.ID = %q, want reliability", b.Meta().ID)
	}
}

// TestDegradesToStaleOnAdminDown is the degrade-don't-crash proof: after a good
// read, an unreachable admin read flags the mirror STALE while still showing the
// last-known pills — and the own poll signal stays live and authoritative.
func TestDegradesToStaleOnAdminDown(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("first collect = %v, want nil", err)
	}
	b.poller.pollOnce(context.Background()) // poll: up

	if body := string(b.Render().Body); strings.Contains(body, "STALE") {
		t.Fatalf("pre-degrade render unexpectedly STALE:\n%s", body)
	}

	// The admin read goes unreachable; the poll target stays up.
	reader.set(goapi.OperatorHealth{}, srcDown("GET /admin/health"))
	if err := collectStore(t, b); err == nil {
		t.Fatal("Collect with admin read down returned nil, want an error so the shell keeps last-known")
	}
	b.poller.pollOnce(context.Background()) // poll still: up

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Errorf("post-degrade render = %q, want a STALE flag", body)
	}
	if !strings.Contains(body, "DB: ok") {
		t.Errorf("post-degrade render dropped last-known pills:\n%s", body)
	}
	if !strings.Contains(body, "REACHABLE") || strings.Contains(body, "UNREACHABLE") {
		t.Errorf("own poll signal degraded with the admin read; it must stay live:\n%s", body)
	}

	// The shell must stay up serving the stale panel and health, wired exactly
	// as production wires it.
	handler := shell.NewHandler(fixedRegistry{[]core.Bucket{b}}).Router(testToken)
	rec := do(handler, authed(httptest.NewRequest(http.MethodGet, "/", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("shell status = %d, want 200 with a stale bucket", rec.Code)
	}
	if sb := rec.Body.String(); !strings.Contains(sb, "STALE") || !strings.Contains(sb, "DB: ok") {
		t.Errorf("shell page missing stale last-known panel:\n%s", sb)
	}
	if h := do(handler, httptest.NewRequest(http.MethodGet, "/health", nil)); h.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200 while admin read down", h.Code)
	}
}

// TestRenderEscapesWatchedAppText is the injection proof: a hostile dependency
// error string in the mirrored health is HTML-escaped, never rendered as markup.
func TestRenderEscapesWatchedAppText(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	evil := "<script>alert('pwn')</script>"
	h := healthyHealth()
	h.DB = "down"
	h.Detail.DBError = evil
	reader.set(h, nil)
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}

	body := string(b.Render().Body)
	if strings.Contains(body, evil) {
		t.Errorf("render leaked unescaped watched-app text:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("render did not HTML-escape watched-app text:\n%s", body)
	}
}

// TestStaysBoundedUnderLoad is the bounded-storage proof: feeding many times the
// ring capacity never grows retained history past the cap.
func TestStaysBoundedUnderLoad(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	b := newBucket(reader, checker, defaultPollInterval)

	for i := 0; i < 4*historyCapacity; i++ {
		if err := collectStore(t, b); err != nil {
			t.Fatalf("collect %d = %v", i, err)
		}
	}
	if got := b.history.Len(); got != historyCapacity {
		t.Errorf("retained %d history samples, want capped at %d", got, historyCapacity)
	}
}

// TestConcurrentCollectAndRender is the concurrency attack: the collect/store
// cycle, the poll and Render run together (as the tick loop, poll goroutine and
// HTTP handlers do) with no data race — run under -race.
func TestConcurrentCollectAndRender(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)
	b := newBucket(reader, checker, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.poller.run(ctx) // the real background poll goroutine

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			signals, _ := b.Collect(ctx)
			b.Store(signals)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = b.Render()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if i%2 == 0 {
				reader.set(goapi.OperatorHealth{}, srcDown("GET /admin/health"))
			} else {
				reader.set(healthyHealth(), nil)
			}
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves the production constructor with no
// go-api env yields a bucket that renders (poll pending, no mirror) and never
// panics — the whole service still starts.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	t.Setenv("OVERSEER_GOAPI_TOKEN", "")
	b := New()
	if _, ok := b.reader.(nullClient); !ok {
		t.Fatalf("unconfigured reader = %T, want nullClient", b.reader)
	}
	if err := collectStore(t, b); err == nil {
		t.Error("Collect with unconfigured go-api returned nil, want source-down error")
	}
	b.poller.pollOnce(context.Background())
	body := string(b.Render().Body)
	if !strings.Contains(body, "UNREACHABLE") {
		t.Errorf("unconfigured render = %q, want UNREACHABLE from the poll", body)
	}
}

func TestPollIntervalFromEnv(t *testing.T) {
	t.Setenv("OVERSEER_RELIABILITY_POLL_INTERVAL", "")
	if got := pollIntervalFromEnv(); got != defaultPollInterval {
		t.Errorf("empty env = %v, want default %v", got, defaultPollInterval)
	}
	t.Setenv("OVERSEER_RELIABILITY_POLL_INTERVAL", "5s")
	if got := pollIntervalFromEnv(); got != 5*time.Second {
		t.Errorf("valid env = %v, want 5s", got)
	}
	t.Setenv("OVERSEER_RELIABILITY_POLL_INTERVAL", "garbage")
	if got := pollIntervalFromEnv(); got != defaultPollInterval {
		t.Errorf("invalid env = %v, want default %v", got, defaultPollInterval)
	}
}

type fixedRegistry struct{ buckets []core.Bucket }

func (f fixedRegistry) Buckets() []core.Bucket { return f.buckets }

func authed(r *http.Request) *http.Request {
	r.Header.Set("Authorization", "Bearer "+testToken)
	return r
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}
