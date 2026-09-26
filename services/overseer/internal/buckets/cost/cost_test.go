package cost

import (
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

// fakeReader is a controllable stand-in for the OCI spend read path. It counts
// reads so a test can prove spend does not ride the collect tick.
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

// fakeUsageReader is a controllable stand-in for the go-api provider-usage read.
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

func liveBucket() (*Bucket, *fakeReader, *fakeUsageReader) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	return newBucket(spend, usage, time.Hour), spend, usage
}

// refreshSpend drives the spend half deterministically, standing in for the slow
// scheduler that owns it in production (which the collect tick never triggers).
func refreshSpend(t *testing.T, b *Bucket) {
	t.Helper()
	b.refreshSpend(context.Background())
}

// waitFor polls cond until it holds or the deadline passes, so a test can assert on
// the background scheduler's progress without a fixed sleep racing a slow machine.
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

// TestCollectStoreSnapshot is the core Done proof: a scheduled spend refresh and a
// live provider-usage collect both flow into the bucket and the snapshot carries
// both halves.
func TestCollectStoreSnapshot(t *testing.T) {
	b, _, _ := liveBucket()

	empty := b.Snapshot()
	if empty.State != core.StateStale {
		t.Errorf("empty state = %q, want stale (nothing mirrored yet)", empty.State)
	}
	ed := snapData(t, empty)
	if ed.Spend != nil || ed.Usage != nil {
		t.Errorf("empty snapshot carried data: spend=%+v usage=%+v", ed.Spend, ed.Usage)
	}

	refreshSpend(t, b)
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

// TestStartPollsSpendAcrossTicks is the cadence-survival proof, driven through the
// REAL Start hook (not a direct refresh call): the scheduler runs on the
// app-lifetime ctx, so it refreshes once immediately AND keeps ticking past the
// first — a second refresh lands, proving it is not once-and-frozen the way the
// prior Collect-launched attempt was after the per-tick deadline cancelled it.
func TestStartPollsSpendAcrossTicks(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	b := newBucket(spend, &fakeUsageReader{}, time.Millisecond)

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

// TestStartLaunchesSchedulerOnce is the double-start proof: the sync.Once means a
// second Start never launches a second poller. The first Start is handed an
// already-cancelled ctx, so its goroutine does exactly one immediate refresh then
// exits; a second Start on a fast-ticking live ctx must NOT add refreshes.
func TestStartLaunchesSchedulerOnce(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	b := newBucket(spend, &fakeUsageReader{}, time.Millisecond)

	dead, cancelDead := context.WithCancel(context.Background())
	cancelDead()
	b.Start(dead)
	waitFor(t, func() bool { return spend.calls.Load() == 1 }, time.Second,
		"first Start did not run its immediate refresh")

	live, cancelLive := context.WithCancel(context.Background())
	defer cancelLive()
	b.Start(live)

	// A rogue second goroutine on the 1ms ticker would drive calls far past 1
	// within this window; the sync.Once must keep it at exactly the one refresh.
	time.Sleep(50 * time.Millisecond)
	if got := spend.calls.Load(); got != 1 {
		t.Errorf("spend refreshed %d times; a second Start launched another poller (want 1, once)", got)
	}
}

// TestSpendNotFetchedOnEveryCollect is the cadence proof: the metered OCI spend
// read is owned by the slow scheduler, never the 5s collect tick, so many collects
// do not multiply usage-api reads. Collect touches only the cheap provider-usage
// half.
func TestSpendNotFetchedOnEveryCollect(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	b := newBucket(spend, usage, time.Hour)

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

// TestSpendServedWithItsAge proves the spend half carries when it last refreshed,
// so the panel can show how old the served figure is between slow refreshes.
func TestSpendServedWithItsAge(t *testing.T) {
	b, _, _ := liveBucket()

	before := time.Now().UTC()
	refreshSpend(t, b)

	d := snapData(t, b.Snapshot())
	if d.SpendUpdatedAt.IsZero() {
		t.Fatal("spendUpdatedAt is zero — a served spend figure carries no age")
	}
	if d.SpendUpdatedAt.Before(before) {
		t.Errorf("spendUpdatedAt = %v, want >= the refresh time %v", d.SpendUpdatedAt, before)
	}
}

// TestSpendDegradesIndependently: OCI down, go-api up -> only the spend half stale,
// last-known spend preserved, provider half live, no collect error.
func TestSpendDegradesIndependently(t *testing.T) {
	b, spend, _ := liveBucket()
	refreshSpend(t, b)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
	refreshSpend(t, b)
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

// TestUsageDegradesIndependently: go-api down, OCI up -> only the provider half
// stale, last-known usage preserved, spend half live, no collect error.
func TestUsageDegradesIndependently(t *testing.T) {
	b, _, usage := liveBucket()
	refreshSpend(t, b)
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

// TestBothDownReturnsError: the usage read fails while the spend half is already
// unreachable -> collect error, source_down state, both halves stale but last-known
// preserved.
func TestBothDownReturnsError(t *testing.T) {
	b, spend, usage := liveBucket()
	refreshSpend(t, b)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
	refreshSpend(t, b)
	usage.set(nil, usageSrcDown())
	err := collectStore(t, b)
	if err == nil {
		t.Fatal("Collect with both sources down returned nil error")
	}
	if !errors.Is(err, errBothDown) {
		t.Errorf("both-down error = %v, want errBothDown", err)
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

// TestSnapshotCarriesRawServiceName proves service names — external usage-api data
// — are carried verbatim in the payload (React escapes them on render).
func TestSnapshotCarriesRawServiceName(t *testing.T) {
	spend := &fakeReader{}
	spend.set(oci.Spend{
		Amount:   1,
		Currency: "USD",
		Lines:    []oci.SpendLine{{Service: `<script>alert(1)</script>`, Amount: 1}},
	}, nil)
	b := newBucket(spend, &fakeUsageReader{}, time.Hour)

	refreshSpend(t, b)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if d.Spend == nil || len(d.Spend.Lines) != 1 || d.Spend.Lines[0].Service != `<script>alert(1)</script>` {
		t.Errorf("service name not carried verbatim: %+v", d.Spend)
	}
}

// TestNoOCIIdentifierInSnapshot proves nothing OCI-identifying reaches the
// payload: the spend model carries no identifier field, so no "ocid1." appears.
func TestNoOCIIdentifierInSnapshot(t *testing.T) {
	b, _, _ := liveBucket()
	refreshSpend(t, b)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if strings.Contains(string(b.Snapshot().Data), "ocid1.") {
		t.Errorf("snapshot leaked an OCI identifier: %s", b.Snapshot().Data)
	}
}

// TestConcurrentCollectAndSnapshot proves the collect loop, the spend scheduler's
// refresh and the HTTP read can run at once without a data race (asserted under
// -race) — the shared last-known state is guarded by the bucket mutex.
func TestConcurrentCollectAndSnapshot(t *testing.T) {
	b, _, _ := liveBucket()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = collectStore(t, b)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			b.refreshSpend(context.Background())
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = b.Snapshot()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves an unconfigured bucket never panics: a
// scheduled spend read and a collect both degrade to source-down.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullSpendReader{}, nullUsageReader{}, time.Hour)

	refreshSpend(t, b)
	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("unconfigured Collect error = %v, want errBothDown", err)
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

// TestSourceErrorsClassifyTyped proves each half keeps its typed source-down
// classification: the usage cause survives inside the aggregate both-down error,
// and the spend read still classifies as OCI source-down.
func TestSourceErrorsClassifyTyped(t *testing.T) {
	b := newBucket(nullSpendReader{}, nullUsageReader{}, time.Hour)
	refreshSpend(t, b)
	_, err := b.Collect(context.Background())
	if err == nil {
		t.Fatal("Collect returned nil on full outage")
	}
	if !errors.Is(err, errUsageUnconfigured) {
		t.Errorf("both-down error dropped the usage cause: %v", err)
	}
	if _, spendErr := (nullSpendReader{}).CurrentPeriodSpend(context.Background()); !oci.IsSourceDown(spendErr) {
		t.Errorf("spend half degrade not classified source-down: %v", spendErr)
	}
}

// TestLazyReaderDegradesOnBuildFailure proves the lazy production OCI reader
// degrades to source-down when the build fails, rather than panicking, and retries.
func TestLazyReaderDegradesOnBuildFailure(t *testing.T) {
	calls := 0
	l := &lazyReader{build: func() (spendReader, error) {
		calls++
		return nil, errors.New("instance principal unavailable")
	}}
	for i := 0; i < 2; i++ {
		_, err := l.CurrentPeriodSpend(context.Background())
		if !oci.IsSourceDown(err) {
			t.Fatalf("lazy read #%d error = %v, want source-down", i, err)
		}
	}
	if calls != 2 {
		t.Errorf("build called %d times, want 2 (retried after failure)", calls)
	}
}

// TestLazyReaderBuildsOnceOnSuccess proves the lazy reader builds the client once.
func TestLazyReaderBuildsOnceOnSuccess(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	calls := 0
	l := &lazyReader{build: func() (spendReader, error) {
		calls++
		return reader, nil
	}}
	for i := 0; i < 3; i++ {
		if _, err := l.CurrentPeriodSpend(context.Background()); err != nil {
			t.Fatalf("lazy read #%d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("build called %d times, want 1 (cached after success)", calls)
	}
}

// TestOCIEnabledParsing proves the enablement gate reads the env flag correctly.
func TestOCIEnabledParsing(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{{"1", true}, {"true", true}, {"TRUE", true}, {"yes", true}, {"on", true}, {"", false}, {"0", false}, {"no", false}} {
		t.Setenv("OVERSEER_OCI_ENABLED", tc.val)
		if got := ociEnabled(); got != tc.want {
			t.Errorf("ociEnabled(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

// TestSpendIntervalFromEnv proves the cadence knob defaults to the slow interval,
// honours a valid override, and rejects a malformed value back to the default
// rather than disabling the refresh.
func TestSpendIntervalFromEnv(t *testing.T) {
	t.Setenv("OVERSEER_COST_SPEND_INTERVAL", "")
	if got := spendIntervalFromEnv(); got != defaultSpendInterval {
		t.Errorf("unset interval = %v, want default %v", got, defaultSpendInterval)
	}

	t.Setenv("OVERSEER_COST_SPEND_INTERVAL", "30m")
	if got := spendIntervalFromEnv(); got != 30*time.Minute {
		t.Errorf("override interval = %v, want 30m", got)
	}

	t.Setenv("OVERSEER_COST_SPEND_INTERVAL", "not-a-duration")
	if got := spendIntervalFromEnv(); got != defaultSpendInterval {
		t.Errorf("malformed interval = %v, want fallback to default %v", got, defaultSpendInterval)
	}
}

// TestSpendSchedulerExitsOnCancel proves the background goroutine drains on ctx
// cancellation — no goroutine leaks past the app's lifetime — after its immediate
// refresh.
func TestSpendSchedulerExitsOnCancel(t *testing.T) {
	var runs atomic.Int32
	s := newSpendScheduler(time.Millisecond, func(context.Context) { runs.Add(1) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("spendScheduler.run did not return within 2s of ctx cancel — goroutine leak")
	}
	if runs.Load() == 0 {
		t.Error("scheduler never ran the immediate refresh")
	}
}

// TestSpendSchedulerContainsPanic proves a panicking refresh is contained: the
// process survives and the scheduler keeps ticking rather than crashing the shell.
func TestSpendSchedulerContainsPanic(t *testing.T) {
	panicking := func(context.Context) { panic("spend read blew up") }

	newSpendScheduler(time.Hour, panicking).safeRefreshOnce(context.Background())

	s := newSpendScheduler(time.Millisecond, panicking)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("spendScheduler.run did not return after panicking refreshes — panic escaped containment")
	}
}

// TestSpendSchedulerClampsInterval proves a non-positive interval cannot disable
// the ticker: it is clamped to the default rather than panicking NewTicker.
func TestSpendSchedulerClampsInterval(t *testing.T) {
	s := newSpendScheduler(0, func(context.Context) {})
	if s.interval != defaultSpendInterval {
		t.Errorf("interval = %v, want clamped to %v", s.interval, defaultSpendInterval)
	}
}

// TestSeverityCriticalWhenAProviderNeverSucceeds is the health-grade proof: both
// reads are fresh and live, but every call to one provider is being rejected —
// a dead integration Altune is still paying for, so the bucket grades itself
// critical while State stays live.
func TestSeverityCriticalWhenAProviderNeverSucceeds(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(goapi.ProviderUsage{
		"deezer":  {OK: 0, Quota: 40, Error: 12},
		"spotify": {OK: 30, Quota: 0, Error: 1},
	}, nil)
	b := newBucket(spend, usage, time.Hour)
	refreshSpend(t, b)
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

// TestSeverityWarnsWhenAProviderMostlyFails proves the middle grade: the provider
// still serves some calls, but it fails more than it serves.
func TestSeverityWarnsWhenAProviderMostlyFails(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(goapi.ProviderUsage{"deezer": {OK: 10, Quota: 20, Error: 5}}, nil)
	b := newBucket(spend, usage, time.Hour)
	refreshSpend(t, b)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := b.Snapshot().Severity; got != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", got)
	}
}

// TestSeverityOKWhenProvidersMostlySucceed is the arm that has to disagree: the
// sample usage carries a few quota rejections and one error, which is normal
// traffic, so it grades ok. A provider with no calls at all is not a fault either.
func TestSeverityOKWhenProvidersMostlySucceed(t *testing.T) {
	b, _, _ := liveBucket()
	refreshSpend(t, b)
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

// TestUsageReaderFromEnvDegradesWhenUnconfigured proves the provider-usage half
// falls back to a null reader when go-api is unconfigured.
func TestUsageReaderFromEnvDegradesWhenUnconfigured(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	t.Setenv("OVERSEER_GOAPI_TOKEN", "")
	if _, ok := usageReaderFromEnv().(nullUsageReader); !ok {
		t.Errorf("unconfigured usageReaderFromEnv = %T, want nullUsageReader", usageReaderFromEnv())
	}
}
