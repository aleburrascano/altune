package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

func TestAdminReasonOutranksPollDegraded(t *testing.T) {
	cases := []struct {
		name     string
		adminErr error
		want     string
	}{
		{"token failure stays auth", &goapi.TokenError{Op: "acquire read-only token", Err: goapi.ErrNoToken}, goapi.ReasonAuth},
		{"forbidden stays auth", &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusForbidden}, goapi.ReasonAuth},
		{"rate limit stays throttled", &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusTooManyRequests}, goapi.ReasonThrottled},
		{"unreachable observe yields to degraded", srcDown("GET /observe/health"), goapi.ReasonDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{}
			reader.set(goapi.OperatorHealth{}, tc.adminErr)
			b, probe := probedBucket(t, reader)
			_ = collectStore(t, b)
			probe.answer(degraded())

			if got := b.Snapshot().Reason; got != tc.want {
				t.Errorf("Reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDependencyDownOutranksPollDegraded(t *testing.T) {
	reader := &fakeReader{}
	down := healthyHealth()
	down.DB = "down"
	reader.set(down, nil)
	b, probe := probedBucket(t, reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	probe.answer(degraded())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical — a down dependency must not be demoted to warn", snap.Severity)
	}
	if snap.Headline != "dependency down · uptime 100.0%" {
		t.Errorf("headline = %q, want dependency down", snap.Headline)
	}
}

func TestDegradedProbeCountsAsReachableForUptime(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(degraded())
	probe.answer(up())

	snap := b.Snapshot()

	if snap.Headline != "uptime 100.0%" {
		t.Errorf("headline = %q, want uptime 100.0%% with no flap", snap.Headline)
	}
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
}

func TestDegradedWindowReportsFullUptime(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(degraded())
	probe.answer(degraded())

	if got := b.Snapshot().Headline; got != "go-api degraded · uptime 100.0%" {
		t.Errorf("headline = %q, want go-api degraded at uptime 100.0%%", got)
	}
}

func TestKeySeriesIsLatencyMS(t *testing.T) {
	if got := newBucket(&fakeReader{}).KeySeries(); got != "latency_ms" {
		t.Errorf("KeySeries() = %q, want latency_ms", got)
	}
}

func TestAnsweredProbeWritesUnderTheDeclaredKeySeries(t *testing.T) {
	b, probe, series := bucketWithSeries(t, nil)

	probe.answer(up())

	name := b.KeySeries()
	if got := series.named(name); len(got) != 1 {
		t.Fatalf("series recorded under KeySeries() (%q) = %+v, want exactly one point", name, got)
	}
}

func TestUnansweredProbeWritesNothingUnderTheDeclaredKeySeries(t *testing.T) {
	b, probe, series := bucketWithSeries(t, nil)

	probe.answer(goapi.Health{}, srcDown("GET /health"))

	name := b.KeySeries()
	if got := series.named(name); len(got) != 0 {
		t.Fatalf("series recorded under KeySeries() (%q) = %+v, want none: probe was unanswered", name, got)
	}
}

func TestTokenFailureReasonIsAuthNeverDown(t *testing.T) {
	reader := &fakeReader{}
	reader.set(goapi.OperatorHealth{}, &goapi.TokenError{Op: "acquire read-only token", Err: goapi.ErrNoToken})

	b := newBucket(reader)
	if err := collectStore(t, b); err == nil {
		t.Fatal("Collect with a failing token source returned nil error")
	}

	snap := b.Snapshot()
	if snap.Reason != "auth" {
		t.Errorf("Reason = %q, want auth", snap.Reason)
	}
	if snap.Reason == "down" {
		t.Fatal("a dead credential classified as down; must be auth")
	}
}

func TestReasonClearsOnRecovery(t *testing.T) {
	reader := &fakeReader{}
	reader.set(goapi.OperatorHealth{}, &goapi.TokenError{Op: "acquire read-only token", Err: goapi.ErrNoToken})

	b := newBucket(reader)
	_ = collectStore(t, b)
	if b.Snapshot().Reason != "auth" {
		t.Fatalf("Reason after failure = %q, want auth", b.Snapshot().Reason)
	}

	reader.set(healthyHealth(), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect after recovery = %v, want nil", err)
	}
	if got := b.Snapshot().Reason; got != "" {
		t.Errorf("Reason after recovery = %q, want empty", got)
	}
}

func ringNamed(t *testing.T, b core.Restorable, name string) *core.RingStore {
	t.Helper()
	ring := b.Rings()[name]
	if ring == nil {
		t.Fatalf("Rings() has no %q ring", name)
	}
	return ring
}

func TestRestoredHistoryAndPollRingsShowInTheSnapshot(t *testing.T) {
	b := reliability.New(reliability.Deps{})
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	history := []core.Signal{{At: at, Kind: "health", Text: "db=up redis=up auth=up (healthy)"}}
	poll := []core.Signal{{At: at, Kind: "poll", Text: "up"}, {At: at.Add(time.Minute), Kind: "poll", Text: "down"}}
	historyRing, pollRing := ringNamed(t, b, "history"), ringNamed(t, b, "poll")

	historyRing.Restore(history)
	pollRing.Restore(poll)

	var data reliability.Data
	if err := json.Unmarshal(b.Snapshot().Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.History) != 1 || data.History[0].Text != history[0].Text {
		t.Fatalf("snapshot history = %+v, want the restored %+v", data.History, history)
	}
	if len(data.Poll) != 2 || data.Poll[0].Text != "up" || data.Poll[1].Text != "down" {
		t.Fatalf("snapshot poll = %+v, want the restored %+v", data.Poll, poll)
	}
}

type recordedPoint struct {
	bucket string
	series string
	point  core.Point
}

type recordingSeries struct {
	mu     sync.Mutex
	points []recordedPoint
}

func (r *recordingSeries) Record(bucket, series string, p core.Point) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points = append(r.points, recordedPoint{bucket: bucket, series: series, point: p})
}

func (r *recordingSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func (r *recordingSeries) named(series string) []core.Point {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []core.Point
	for _, p := range r.points {
		if p.bucket == "reliability" && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

type steppingClock struct {
	at   time.Time
	step time.Duration
}

func (c *steppingClock) now() time.Time {
	current := c.at
	c.at = c.at.Add(c.step)
	return current
}

func bucketWithSeries(t *testing.T, now func() time.Time) (*reliability.Bucket, *probes, *recordingSeries) {
	t.Helper()
	probe := newProbes(t)
	b := reliability.New(reliability.Deps{Reader: &fakeReader{}, Checker: probe, Interval: fastInterval, Now: now})
	series := &recordingSeries{}
	b.UseSeries(series)
	probe.start(b)
	return b, probe, series
}

func TestReachableProbeRecordsUpAndItsLatency(t *testing.T) {
	clock := &steppingClock{at: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), step: 42 * time.Millisecond}
	_, probe, series := bucketWithSeries(t, clock.now)

	probe.answer(up())

	up := series.named("up")
	if len(up) != 1 || up[0].Value != 1 {
		t.Fatalf("up series = %+v, want one point of 1", up)
	}
	latency := series.named("latency_ms")
	if len(latency) != 1 || latency[0].Value != 42 {
		t.Fatalf("latency_ms series = %+v, want one point of 42", latency)
	}
	if !latency[0].At.Equal(up[0].At) {
		t.Fatalf("latency at %v and up at %v differ; one probe is one instant", latency[0].At, up[0].At)
	}
	if want := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).Add(42 * time.Millisecond); !up[0].At.Equal(want) {
		t.Fatalf("probe recorded at %v, want its finish time %v", up[0].At, want)
	}
}

func TestUnreachableProbeRecordsDownWithoutALatency(t *testing.T) {
	_, probe, series := bucketWithSeries(t, nil)

	probe.answer(goapi.Health{}, srcDown("GET /health"))

	if up := series.named("up"); len(up) != 1 || up[0].Value != 0 {
		t.Fatalf("up series = %+v, want one point of 0", up)
	}
	if latency := series.named("latency_ms"); len(latency) != 0 {
		t.Fatalf("latency_ms series = %+v, want none: a probe with no answer has no latency", latency)
	}
}

func TestAnsweredDegradedProbeRecordsUpAndItsLatency(t *testing.T) {
	_, probe, series := bucketWithSeries(t, nil)

	probe.answer(degraded())

	if up := series.named("up"); len(up) != 1 || up[0].Value != 1 {
		t.Fatalf("up series = %+v, want one point of 1 (degraded is reachable)", up)
	}
	if latency := series.named("latency_ms"); len(latency) != 1 {
		t.Fatalf("latency_ms series = %+v, want one point: go-api answered", latency)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(&fakeReader{})
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("reliability does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestProbeWithoutSeriesStillPolls(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(up())

	if got := snapData(t, b.Snapshot()).Reachability; got != "up" {
		t.Fatalf("reachability = %q, want up with no history store wired", got)
	}
}

var _ core.Waiter = (*reliability.Bucket)(nil)

func TestWaitReturnsOnceThePollerHasStopped(t *testing.T) {
	b := reliability.New(reliability.Deps{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	cancel()

	joined := make(chan struct{})
	go func() {
		b.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the poller's ctx was cancelled")
	}
}
