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

// fakeReader is a controllable stand-in for the admin-read (mirror) path.
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

// fakeChecker is a controllable stand-in for the independent reachability poll. It
// shares no field with fakeReader, which is the whole point.
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

// TestMirrorCollectStoreSnapshot is the core Done proof: a healthy operator-health
// read flows into the mirror and the snapshot carries DB/Redis/Auth plus a bounded
// history sample, with the own poll reachability set live.
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

// TestDegradesToStaleOnAdminDown is the degrade-don't-crash proof: after a good
// read, an unreachable admin read (poll still up) flags the panel stale while
// still carrying the last-known pills — and the own poll stays authoritative live.
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

	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-degrade state = %q, want live", snap.State)
	}

	reader.set(goapi.OperatorHealth{}, srcDown("GET /observe/health"))
	if err := collectStore(t, b); err == nil {
		t.Fatal("Collect with admin read down returned nil, want an error so the shell keeps last-known")
	}
	b.poller.pollOnce(context.Background()) // poll still: up

	snap := b.Snapshot()
	// Poll authoritative up + admin mirror stale = stale (not source_down).
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

// TestSnapshotCarriesRawText is the injection proof: a hostile dependency error
// string is carried VERBATIM in the JSON (React escapes it on render).
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

// TestStaysBoundedUnderLoad is the bounded-storage proof.
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

// TestConcurrentCollectAndSnapshot is the concurrency attack — run under -race.
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

// TestUnconfiguredDegradesNotCrashes proves the production constructor with no
// go-api env yields a bucket that reports source_down (poll down) and never panics.
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

// TestSeverityCriticalWhenDependencyDown is the health-grade proof: go-api is
// reachable and the admin mirror is perfectly fresh, but go-api reports its
// database down — so the bucket grades itself critical while State stays live.
// Freshness and health are separate answers.
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

// TestSeverityOKWhenEveryDependencyHealthy is the other arm: the same probe
// window with nothing down grades ok, so the critical above is about the payload
// and not about the bucket always being red.
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

// TestSeverityWarnsAfterAFlap proves the middle grade: the app is up right now
// but the bounded probe window still holds a failed probe, which is worth a look
// rather than a page.
func TestSeverityWarnsAfterAFlap(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	b := newBucket(reader, checker, defaultPollInterval)

	// The two manual probes below are the whole window this test grades, so it
	// must not go through Collect: that starts the background poll goroutine,
	// whose own immediate probe would land a third, unordered sample in the ring
	// and turn the intended 1-down/1-up window into a flaky ratio.
	checker.set(goapi.Health{}, srcDown("GET /health"))
	b.poller.pollOnce(context.Background()) // probe: down
	checker.set(goapi.Health{Status: "ok"}, nil)
	b.poller.pollOnce(context.Background()) // probe: back up

	snap := b.Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if snap.Headline != "recently flapped · uptime 50.0%" {
		t.Errorf("headline = %q, want the flap named with its uptime", snap.Headline)
	}
}

// countingChecker records how many reachability probes the poller has fired, so a
// test can prove the poll loop keeps running past the first tick and stops once
// its ctx is cancelled.
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

// TestStartKeepsPollerRunningPastFirstTick drives the reachability poller through
// the real app-lifetime Start hook (#1950) and proves it keeps probing after the
// first collect tick's ctx is cancelled. Before #1950 the poller was launched from
// Collect with the per-tick collect-timeout ctx (#1812), so it froze after one
// run; here a climbing probe count proves it now runs on the app ctx. Cancelling
// that ctx returns the poll goroutine, so the count stops climbing (no leak).
func TestStartKeepsPollerRunningPastFirstTick(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)
	checker := &countingChecker{}
	b := newBucket(reader, checker, time.Millisecond)

	appCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b.Start(appCtx)

	// First mirror tick on its own short-lived ctx, then cancel it — the #1812
	// per-bucket collect deadline. Under the regression the poller ran on this ctx
	// and froze here; on the Start hook it keeps probing on the app ctx.
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
	time.Sleep(50 * time.Millisecond) // many poll intervals at 1ms
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
