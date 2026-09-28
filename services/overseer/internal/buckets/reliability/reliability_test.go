package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func collectStore(t *testing.T, b *Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

func snapData(t *testing.T, snap core.Snapshot) Data {
	t.Helper()
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func TestMirrorCollectStoreSnapshot(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)

	if d := snapData(t, b.Snapshot()); d.Health != nil {
		t.Errorf("empty snapshot health = %+v, want nil", d.Health)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while healthy = %v, want nil", err)
	}
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if d.Reachability != "up" {
		t.Errorf("reachability = %q, want up", d.Reachability)
	}
	if d.Health == nil || d.Health.DB != "ok" || d.Health.Redis != "ok" || d.Health.Auth != "ok" {
		t.Errorf("health pills = %+v, want all ok", d.Health)
	}
	if len(d.History) != 1 {
		t.Errorf("history = %d, want 1 sample", len(d.History))
	}
	if b.Meta().ID != "reliability" {
		t.Errorf("Meta.ID = %q, want reliability", b.Meta().ID)
	}
}

func TestDegradesToStaleOnAdminDown(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("first collect = %v, want nil", err)
	}
	b.poller.pollOnce(context.Background())

	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-degrade state = %q, want live", snap.State)
	}

	reader.set(goapi.OperatorHealth{}, srcDown("GET /observe/health"))
	if err := collectStore(t, b); err == nil {
		t.Fatal("Collect with admin read down returned nil, want an error so the shell keeps last-known")
	}
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()
	if snap.State != core.StateStale {
		t.Errorf("post-degrade state = %q, want stale", snap.State)
	}
	d := snapData(t, snap)
	if !d.AdminStale {
		t.Error("adminStale = false, want true")
	}
	if d.Health == nil || d.Health.DB != "ok" {
		t.Errorf("post-degrade dropped last-known pills: %+v", d.Health)
	}
	if d.Reachability != "up" {
		t.Errorf("own poll degraded with the admin read: reachability = %q, want up", d.Reachability)
	}
}

func TestSnapshotCarriesRawText(t *testing.T) {
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

	d := snapData(t, b.Snapshot())
	if d.Health == nil || d.Health.Detail.DBError != evil {
		t.Errorf("watched-app text not carried verbatim: %+v", d.Health)
	}
}

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

func TestConcurrentCollectAndSnapshot(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)
	b := newBucket(reader, checker, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.poller.run(ctx)

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
			_ = b.Snapshot()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if i%2 == 0 {
				reader.set(goapi.OperatorHealth{}, srcDown("GET /observe/health"))
			} else {
				reader.set(healthyHealth(), nil)
			}
		}
	}()
	wg.Wait()
}

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
	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
	if d := snapData(t, snap); d.Reachability != "down" {
		t.Errorf("reachability = %q, want down", d.Reachability)
	}
}

func TestSeverityCriticalWhenDependencyDown(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	degraded := healthyHealth()
	degraded.DB = "down"
	reader.set(degraded, nil)
	checker.set(goapi.Health{Status: "ok"}, nil)
	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — a down dependency is not a stale source", snap.State)
	}
	if !strings.Contains(snap.Headline, "uptime") {
		t.Errorf("headline = %q, want the uptime figure", snap.Headline)
	}
}

func TestSeverityOKWhenEveryDependencyHealthy(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "ok"}, nil)
	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "uptime 100.0%" {
		t.Errorf("headline = %q, want uptime 100.0%%", snap.Headline)
	}
}

func TestSeverityWarnsAfterAFlap(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	b := newBucket(reader, checker, defaultPollInterval)

	checker.set(goapi.Health{}, srcDown("GET /health"))
	b.poller.pollOnce(context.Background())
	checker.set(goapi.Health{Status: "ok"}, nil)
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if snap.Headline != "recently flapped · uptime 50.0%" {
		t.Errorf("headline = %q, want the flap named with its uptime", snap.Headline)
	}
}

type countingChecker struct {
	mu    sync.Mutex
	count int
}

func (c *countingChecker) Health(context.Context) (goapi.Health, error) {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return goapi.Health{Status: "ok"}, nil
}

func (c *countingChecker) probes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func TestStartKeepsPollerRunningPastFirstTick(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)
	checker := &countingChecker{}
	b := newBucket(reader, checker, time.Millisecond)

	appCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b.Start(appCtx)

	firstCtx, firstCancel := context.WithCancel(context.Background())
	_, _ = b.Collect(firstCtx)
	firstCancel()

	deadline := time.After(2 * time.Second)
	for checker.probes() < 5 {
		select {
		case <-deadline:
			t.Fatalf("poller fired %d probes then froze after the first tick's ctx was cancelled — the #1812 regression", checker.probes())
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	settled := checker.probes()
	time.Sleep(50 * time.Millisecond)
	if got := checker.probes(); got > settled+1 {
		t.Fatalf("poller kept probing after app ctx cancel: %d -> %d — leaked past shutdown", settled, got)
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
