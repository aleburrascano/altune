package cost_test

import (
	"altune/overseer/internal/buckets/cost"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/oci"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeReader struct {
	mu    sync.Mutex
	spend oci.Spend
	err   error
	calls atomic.Int64
}

func (f *fakeReader) CurrentPeriodSpend(context.Context) (oci.Spend, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spend, f.err
}

func (f *fakeReader) set(spend oci.Spend, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spend, f.err = spend, err
}

type fakeUsageReader struct {
	mu    sync.Mutex
	usage goapi.ProviderUsage
	err   error
}

func (f *fakeUsageReader) AdminProviderUsage(context.Context) (goapi.ProviderUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage, f.err
}

func (f *fakeUsageReader) set(usage goapi.ProviderUsage, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage, f.err = usage, err
}

func srcDown() error {
	return &oci.SourceDownError{Err: errors.New("dial refused")}
}

func usageSrcDown() error {
	return &goapi.SourceDownError{Op: "GET /observe/metrics/live", Err: errors.New("dial refused")}
}

func sampleSpend() oci.Spend {
	return oci.Spend{
		Amount:      41.50,
		Currency:    "USD",
		PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
		Lines: []oci.SpendLine{
			{Service: "STORAGE", Amount: 25},
			{Service: "COMPUTE", Amount: 16.50},
		},
	}
}

func sampleUsage() goapi.ProviderUsage {
	return goapi.ProviderUsage{
		"deezer":  {OK: 120, Quota: 4, Error: 2},
		"spotify": {OK: 30, Quota: 0, Error: 1},
		"other":   {OK: 0, Quota: 0, Error: 0},
	}
}

func liveBucket() (*cost.Bucket, *fakeReader, *fakeUsageReader) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	return newBucket(spend, usage), spend, usage
}

type usageReader interface {
	AdminProviderUsage(ctx context.Context) (goapi.ProviderUsage, error)
}

func connected(reader cost.SpendReader) func() (cost.SpendReader, error) {
	return func() (cost.SpendReader, error) { return reader, nil }
}

func newBucket(spend *fakeReader, usage usageReader) *cost.Bucket {
	return cost.New(cost.Deps{ConnectSpend: connected(spend), Usage: usage, SpendInterval: time.Millisecond})
}

func refreshSpend(t *testing.T, b *cost.Bucket, spend *fakeReader) {
	t.Helper()
	b.Start(t.Context())
	from := spend.calls.Load()
	waitFor(t, func() bool { return spend.calls.Load() >= from+2 }, 2*time.Second, "the spend scheduler stopped reading spend")
}

func unconfigured(t *testing.T) *cost.Bucket {
	t.Helper()
	b := cost.New(cost.Deps{SpendInterval: time.Millisecond})
	b.Start(t.Context())
	waitFor(t, func() bool { return snapData(t, b.Snapshot()).SpendStale }, 2*time.Second, "spend never marked stale with no OCI connection")
	return b
}

func waitFor(t *testing.T, cond func() bool, within time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(msg)
}

func collectStore(t *testing.T, b *cost.Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

func snapData(t *testing.T, snap core.Snapshot) cost.Data {
	t.Helper()
	var d cost.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func TestCollectStoreSnapshot(t *testing.T) {
	b, spend, _ := liveBucket()

	empty := b.Snapshot()
	if empty.State != core.StateStale {
		t.Errorf("empty state = %q, want stale (nothing mirrored yet)", empty.State)
	}
	ed := snapData(t, empty)
	if ed.Spend != nil || ed.Usage != nil {
		t.Errorf("empty snapshot carried data: spend=%+v usage=%+v", ed.Spend, ed.Usage)
	}

	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while both live = %v, want nil", err)
	}
	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if d.Spend == nil || d.Spend.Amount != 41.50 || d.Spend.Currency != "USD" {
		t.Errorf("spend = %+v, want 41.50 USD", d.Spend)
	}
	if d.Usage == nil || (*d.Usage)["deezer"].OK != 120 {
		t.Errorf("usage = %+v, want deezer ok=120", d.Usage)
	}
}

func TestStartPollsSpendAcrossTicks(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	b := newBucket(spend, &fakeUsageReader{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)

	waitFor(t, func() bool { return spend.calls.Load() >= 2 }, 2*time.Second,
		"spend refreshed fewer than twice through Start — the scheduler froze after one run")
	d := snapData(t, b.Snapshot())
	if d.Spend == nil || d.Spend.Amount != 41.50 {
		t.Errorf("scheduled spend did not flow into the snapshot: %+v", d.Spend)
	}
}

func TestStartLaunchesSchedulerOnce(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	b := newBucket(spend, &fakeUsageReader{})

	dead, cancelDead := context.WithCancel(context.Background())
	cancelDead()
	b.Start(dead)
	waitFor(t, func() bool { return spend.calls.Load() == 1 }, time.Second,
		"first Start did not run its immediate refresh")

	live, cancelLive := context.WithCancel(context.Background())
	defer cancelLive()
	b.Start(live)

	time.Sleep(50 * time.Millisecond)
	if got := spend.calls.Load(); got != 1 {
		t.Errorf("spend refreshed %d times; a second Start launched another poller (want 1, once)", got)
	}
}

func TestSpendNotFetchedOnEveryCollect(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	b := newBucket(spend, usage)

	const collects = 50
	for i := 0; i < collects; i++ {
		if _, err := b.Collect(context.Background()); err != nil {
			t.Fatalf("collect %d: %v", i, err)
		}
	}

	if got := spend.calls.Load(); got != 0 {
		t.Errorf("spend fetched %d times across %d collects — Collect must not read spend", got, collects)
	}
}

func TestSpendServedWithItsAge(t *testing.T) {
	b, spend, _ := liveBucket()

	before := time.Now().UTC()
	refreshSpend(t, b, spend)

	d := snapData(t, b.Snapshot())
	if d.SpendUpdatedAt.IsZero() {
		t.Fatal("spendUpdatedAt is zero — a served spend figure carries no age")
	}
	if d.SpendUpdatedAt.Before(before) {
		t.Errorf("spendUpdatedAt = %v, want >= the refresh time %v", d.SpendUpdatedAt, before)
	}
}

func TestSpendDegradesIndependently(t *testing.T) {
	b, spend, _ := liveBucket()
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect with only OCI down = %v, want nil (independent degrade)", err)
	}
	d := snapData(t, b.Snapshot())
	if !d.SpendStale {
		t.Error("spendStale = false, want true")
	}
	if d.UsageStale {
		t.Error("usageStale = true, want false (go-api was up)")
	}
	if d.Spend == nil || d.Spend.Amount != 41.50 {
		t.Errorf("stale spend dropped the last-known figure: %+v", d.Spend)
	}
}

func TestUsageDegradesIndependently(t *testing.T) {
	b, spend, usage := liveBucket()
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	usage.set(nil, usageSrcDown())
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect with only go-api down = %v, want nil (independent degrade)", err)
	}
	d := snapData(t, b.Snapshot())
	if !d.UsageStale {
		t.Error("usageStale = false, want true")
	}
	if d.SpendStale {
		t.Error("spendStale = true, want false (OCI was up)")
	}
	if d.Usage == nil || (*d.Usage)["deezer"].OK != 120 {
		t.Errorf("stale usage dropped last-known counts: %+v", d.Usage)
	}
}

func TestBothDownReturnsError(t *testing.T) {
	b, spend, usage := liveBucket()
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
	refreshSpend(t, b, spend)
	usage.set(nil, usageSrcDown())
	err := collectStore(t, b)
	if err == nil {
		t.Fatal("Collect with both sources down returned nil error")
	}
	if !errors.Is(err, cost.ErrBothDown) {
		t.Errorf("both-down error = %v, want cost.ErrBothDown", err)
	}
	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("state = %q, want source_down", snap.State)
	}
	d := snapData(t, snap)
	if !d.SpendStale || !d.UsageStale {
		t.Errorf("both halves not stale: spend=%v usage=%v", d.SpendStale, d.UsageStale)
	}
	if d.Spend == nil || d.Usage == nil {
		t.Errorf("full outage dropped last-known values: spend=%+v usage=%+v", d.Spend, d.Usage)
	}
}

func TestSnapshotCarriesRawServiceName(t *testing.T) {
	spend := &fakeReader{}
	spend.set(oci.Spend{
		Amount:   1,
		Currency: "USD",
		Lines:    []oci.SpendLine{{Service: `<script>alert(1)</script>`, Amount: 1}},
	}, nil)
	b := newBucket(spend, &fakeUsageReader{})

	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if d.Spend == nil || len(d.Spend.Lines) != 1 || d.Spend.Lines[0].Service != `<script>alert(1)</script>` {
		t.Errorf("service name not carried verbatim: %+v", d.Spend)
	}
}

func TestNoOCIIdentifierInSnapshot(t *testing.T) {
	b, spend, _ := liveBucket()
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if strings.Contains(string(b.Snapshot().Data), "ocid1.") {
		t.Errorf("snapshot leaked an OCI identifier: %s", b.Snapshot().Data)
	}
}

func TestConcurrentCollectAndSnapshot(t *testing.T) {
	b, _, _ := liveBucket()
	b.Start(t.Context())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			_ = collectStore(t, b)
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			_ = b.Snapshot()
		}
	}()
	wg.Wait()
}

func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := unconfigured(t)

	if _, err := b.Collect(context.Background()); !errors.Is(err, cost.ErrBothDown) {
		t.Fatalf("unconfigured Collect error = %v, want ErrBothDown", err)
	}
	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
	d := snapData(t, snap)
	if d.Spend != nil || d.Usage != nil {
		t.Errorf("unconfigured snapshot carried data: %+v", d)
	}
}

func TestBothDownKeepsTheUsageCause(t *testing.T) {
	b := unconfigured(t)

	_, err := b.Collect(context.Background())
	if err == nil {
		t.Fatal("Collect returned nil on full outage")
	}
	if !strings.Contains(err.Error(), "cost: go-api not configured") {
		t.Errorf("both-down error dropped the usage cause: %v", err)
	}
	if !goapi.IsSourceDown(err) {
		t.Errorf("both-down error = %v, want the usage half classified source-down", err)
	}
}

func TestSpendConnectionIsRetriedAfterAFailure(t *testing.T) {
	var connects atomic.Int64
	b := cost.New(cost.Deps{
		ConnectSpend: func() (cost.SpendReader, error) {
			connects.Add(1)
			return nil, errors.New("instance principal unavailable")
		},
		Usage:         &fakeUsageReader{},
		SpendInterval: time.Millisecond,
	})
	b.Start(t.Context())

	waitFor(t, func() bool { return connects.Load() >= 2 }, 2*time.Second, "a failed spend connection was not retried")
	if d := snapData(t, b.Snapshot()); !d.SpendStale || d.Spend != nil {
		t.Errorf("spend with no connection = stale %v, %+v; want stale and no figure", d.SpendStale, d.Spend)
	}
}

func TestSpendConnectsOnceOnSuccess(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	var connects atomic.Int64
	b := cost.New(cost.Deps{
		ConnectSpend: func() (cost.SpendReader, error) {
			connects.Add(1)
			return reader, nil
		},
		Usage:         &fakeUsageReader{},
		SpendInterval: time.Millisecond,
	})
	b.Start(t.Context())

	waitFor(t, func() bool { return reader.calls.Load() >= 3 }, 2*time.Second, "spend was not read three times")
	if got := connects.Load(); got != 1 {
		t.Errorf("connected %d times, want 1 (kept after success)", got)
	}
}

func TestSpendReadsStopWhenTheContextEnds(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b := newBucket(reader, &fakeUsageReader{})
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	waitFor(t, func() bool { return reader.calls.Load() > 0 }, 2*time.Second, "the scheduler never ran the immediate refresh")

	cancel()
	time.Sleep(20 * time.Millisecond)
	settled := reader.calls.Load()
	time.Sleep(20 * time.Millisecond)
	if got := reader.calls.Load(); got != settled {
		t.Errorf("spend read %d more times after the context ended, want 0", got-settled)
	}
}

type panickingReader struct{ calls atomic.Int64 }

func (p *panickingReader) CurrentPeriodSpend(context.Context) (oci.Spend, error) {
	p.calls.Add(1)
	panic("spend read blew up")
}

func TestSpendRefreshSurvivesAPanickingRead(t *testing.T) {
	reader := &panickingReader{}
	b := cost.New(cost.Deps{ConnectSpend: connected(reader), Usage: &fakeUsageReader{}, SpendInterval: time.Millisecond})
	b.Start(t.Context())

	waitFor(t, func() bool { return reader.calls.Load() >= 3 }, 2*time.Second, "the scheduler stopped after a panicking read")
}

func TestANonPositiveSpendIntervalFallsBackToHourly(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		reader := &fakeReader{}
		reader.set(sampleSpend(), nil)
		b := cost.New(cost.Deps{ConnectSpend: connected(reader), Usage: &fakeUsageReader{}, SpendInterval: interval})
		b.Start(t.Context())

		waitFor(t, func() bool { return reader.calls.Load() == 1 }, 2*time.Second, "the scheduler never ran the immediate refresh")
		time.Sleep(20 * time.Millisecond)
		if got := reader.calls.Load(); got != 1 {
			t.Errorf("interval %v: spend read %d times in 20ms, want only the immediate read", interval, got)
		}
	}
}

func TestSeverityCriticalWhenAProviderNeverSucceeds(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(goapi.ProviderUsage{
		"deezer":  {OK: 0, Quota: 40, Error: 12},
		"spotify": {OK: 30, Quota: 0, Error: 1},
	}, nil)
	b := newBucket(spend, usage)
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — a failing provider is not a stale source", snap.State)
	}
	if snap.Headline != "41.50 USD month-to-date · 83 provider calls" {
		t.Errorf("headline = %q, want both halves' money figures", snap.Headline)
	}
}

func TestSeverityWarnsWhenAProviderMostlyFails(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(goapi.ProviderUsage{"deezer": {OK: 10, Quota: 20, Error: 5}}, nil)
	b := newBucket(spend, usage)
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := b.Snapshot().Severity; got != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", got)
	}
}

func TestSeverityOKWhenProvidersMostlySucceed(t *testing.T) {
	b, spend, _ := liveBucket()
	refreshSpend(t, b, spend)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "41.50 USD month-to-date · 157 provider calls" {
		t.Errorf("headline = %q, want both halves' money figures", snap.Headline)
	}
}
