package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
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

const (
	historyCapacity = 120
	pollCapacity    = 120
	waitLimit       = 5 * time.Second
	fastInterval    = time.Nanosecond
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
	calls  int
}

func (f *fakeChecker) Health(context.Context) (goapi.Health, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.health, f.err
}

func (f *fakeChecker) set(h goapi.Health, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.health, f.err = h, err
}

func (f *fakeChecker) probes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type reply struct {
	health goapi.Health
	err    error
}

type probes struct {
	t       *testing.T
	waiting chan struct{}
	replies chan reply
}

func newProbes(t *testing.T) *probes {
	return &probes{t: t, waiting: make(chan struct{}), replies: make(chan reply)}
}

func (p *probes) Health(ctx context.Context) (goapi.Health, error) {
	select {
	case p.waiting <- struct{}{}:
	case <-ctx.Done():
		return goapi.Health{}, ctx.Err()
	}
	select {
	case r := <-p.replies:
		return r.health, r.err
	case <-ctx.Done():
		return goapi.Health{}, ctx.Err()
	}
}

func (p *probes) start(b *reliability.Bucket) {
	p.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	p.t.Cleanup(func() {
		cancel()
		b.Wait()
	})
	b.Start(ctx)
	p.await()
}

func (p *probes) await() {
	p.t.Helper()
	select {
	case <-p.waiting:
	case <-time.After(waitLimit):
		p.t.Fatal("the poller never reached its next go-api probe")
	}
}

func (p *probes) answer(h goapi.Health, err error) {
	p.t.Helper()
	select {
	case p.replies <- reply{health: h, err: err}:
	case <-time.After(waitLimit):
		p.t.Fatal("the poller never took the probe answer")
	}
	p.await()
}

func probedBucket(t *testing.T, reader *fakeReader) (*reliability.Bucket, *probes) {
	t.Helper()
	p := newProbes(t)
	b := reliability.New(reliability.Deps{Reader: reader, Checker: p, Interval: fastInterval})
	p.start(b)
	return b, p
}

func newBucket(reader *fakeReader) *reliability.Bucket {
	return reliability.New(reliability.Deps{Reader: reader})
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func up() (goapi.Health, error) { return goapi.Health{Status: "ok"}, nil }

func degraded() (goapi.Health, error) { return goapi.Health{Status: "degraded"}, nil }

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

func collectStore(t *testing.T, b *reliability.Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

func snapData(t *testing.T, snap core.Snapshot) reliability.Data {
	t.Helper()
	var d reliability.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func TestMirrorCollectStoreSnapshot(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)

	b, probe := probedBucket(t, reader)

	if d := snapData(t, b.Snapshot()); d.Health != nil {
		t.Errorf("empty snapshot health = %+v, want nil", d.Health)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while healthy = %v, want nil", err)
	}
	probe.answer(up())

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
	if !snap.UpdatedAt.Equal(healthyHealth().Detail.CheckedAt) {
		t.Errorf("UpdatedAt = %v, want the health check time %v", snap.UpdatedAt, healthyHealth().Detail.CheckedAt)
	}
	if b.Meta().ID != "reliability" {
		t.Errorf("Meta.ID = %q, want reliability", b.Meta().ID)
	}
}

func TestDegradesToStaleOnAdminDown(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)

	b, probe := probedBucket(t, reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("first collect = %v, want nil", err)
	}
	probe.answer(up())

	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-degrade state = %q, want live", snap.State)
	}

	reader.set(goapi.OperatorHealth{}, srcDown("GET /observe/health"))
	if err := collectStore(t, b); err == nil {
		t.Fatal("Collect with admin read down returned nil, want an error so the shell keeps last-known")
	}
	probe.answer(up())

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
	evil := "<script>alert('pwn')</script>"
	h := healthyHealth()
	h.DB = "down"
	h.Detail.DBError = evil
	reader.set(h, nil)

	b := newBucket(reader)
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
	reader.set(healthyHealth(), nil)
	b := newBucket(reader)

	for i := 0; i < 4*historyCapacity; i++ {
		if err := collectStore(t, b); err != nil {
			t.Fatalf("collect %d = %v", i, err)
		}
	}
	if got := len(snapData(t, b.Snapshot()).History); got != historyCapacity {
		t.Errorf("retained %d history samples, want capped at %d", got, historyCapacity)
	}
}

func TestConcurrentCollectAndSnapshot(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(up())
	b := reliability.New(reliability.Deps{Reader: reader, Checker: checker, Interval: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		b.Wait()
	})
	b.Start(ctx)

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
	b := reliability.New(reliability.Deps{})

	err := collectStore(t, b)
	var down *goapi.SourceDownError
	if !errors.As(err, &down) || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Collect with no go-api = %v, want the not-configured source-down error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		b.Wait()
	})
	b.Start(ctx)
	eventually(t, "the unconfigured probe to read down", func() bool {
		return snapData(t, b.Snapshot()).Reachability == "down"
	})

	if snap := b.Snapshot(); snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
}

func TestSeverityCriticalWhenDependencyDown(t *testing.T) {
	reader := &fakeReader{}
	down := healthyHealth()
	down.DB = "down"
	reader.set(down, nil)
	b, probe := probedBucket(t, reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	probe.answer(up())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — a down dependency is not a stale source", snap.State)
	}
	if snap.Headline != "dependency down · uptime 100.0%" {
		t.Errorf("headline = %q, want dependency down with the uptime figure", snap.Headline)
	}
}

func TestSeverityOKWhenEveryDependencyHealthy(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)
	b, probe := probedBucket(t, reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	probe.answer(up())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "uptime 100.0%" {
		t.Errorf("headline = %q, want uptime 100.0%%", snap.Headline)
	}
}

func TestSeverityWarnsAfterAFlap(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(goapi.Health{}, srcDown("GET /health"))
	probe.answer(up())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if snap.Headline != "recently flapped · uptime 50.0%" {
		t.Errorf("headline = %q, want the flap named with its uptime", snap.Headline)
	}
}

func TestSeverityCriticalWhileGoAPIIsUnreachable(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(up())
	probe.answer(goapi.Health{}, srcDown("GET /health"))

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.Headline != "go-api unreachable · uptime 50.0%" {
		t.Errorf("headline = %q, want go-api unreachable with its uptime", snap.Headline)
	}
}

func TestNoProbesYetIsSaidInTheHeadline(t *testing.T) {
	snap := newBucket(&fakeReader{}).Snapshot()

	if snap.Headline != "no reachability probes yet" {
		t.Errorf("headline = %q, want no reachability probes yet", snap.Headline)
	}
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
}

func TestStartKeepsPollerRunningPastFirstTick(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)
	checker := &fakeChecker{}
	checker.set(up())
	b := reliability.New(reliability.Deps{Reader: reader, Checker: checker, Interval: time.Millisecond})

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
	b.Wait()
	settled := checker.probes()
	time.Sleep(50 * time.Millisecond)
	if got := checker.probes(); got != settled {
		t.Fatalf("poller kept probing after app ctx cancel: %d -> %d — leaked past shutdown", settled, got)
	}
}

func TestANonPositiveIntervalPollsAtTheDefaultCadence(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		t.Run(interval.String(), func(t *testing.T) {
			checker := &fakeChecker{}
			checker.set(up())
			b := reliability.New(reliability.Deps{Checker: checker, Interval: interval})
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(func() {
				cancel()
				b.Wait()
			})

			b.Start(ctx)
			eventually(t, "the first probe", func() bool { return checker.probes() == 1 })
			time.Sleep(50 * time.Millisecond)

			if got := checker.probes(); got != 1 {
				t.Fatalf("interval %v probed %d times in 50ms, want 1: the default cadence is seconds apart", interval, got)
			}
		})
	}
}
