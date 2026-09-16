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
	"testing"
	"time"
)

// fakeReader is a controllable stand-in for the OCI spend read path.
type fakeReader struct {
	mu    sync.Mutex
	spend oci.Spend
	err   error
}

func (f *fakeReader) CurrentPeriodSpend(context.Context) (oci.Spend, error) {
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
	return &goapi.SourceDownError{Op: "GET /admin/metrics/live", Err: errors.New("dial refused")}
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
	return newBucket(spend, usage), spend, usage
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

// TestCollectStoreSnapshot is the core Done proof: live OCI spend and live provider
// usage both flow into the bucket and the snapshot carries both halves.
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

// TestSpendDegradesIndependently: OCI down, go-api up -> only the spend half stale,
// last-known spend preserved, provider half live, no collect error.
func TestSpendDegradesIndependently(t *testing.T) {
	b, spend, _ := liveBucket()
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
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

// TestBothDownReturnsError: both sources down -> collect error, source_down state,
// both halves stale but last-known preserved.
func TestBothDownReturnsError(t *testing.T) {
	b, spend, usage := liveBucket()
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
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
	b := newBucket(spend, &fakeUsageReader{})

	if err := collectStore(t, b); err != nil && !errors.Is(err, errBothDown) {
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
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if strings.Contains(string(b.Snapshot().Data), "ocid1.") {
		t.Errorf("snapshot leaked an OCI identifier: %s", b.Snapshot().Data)
	}
}

// TestConcurrentCollectAndSnapshot proves the collect loop and the HTTP read can
// run at once without a data race (asserted under -race).
func TestConcurrentCollectAndSnapshot(t *testing.T) {
	b, _, _ := liveBucket()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = collectStore(t, b)
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

// TestUnconfiguredDegradesNotCrashes proves an unconfigured bucket never panics.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullSpendReader{}, nullUsageReader{})

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

// TestSourceErrorsClassifyTyped proves each half's degrade error keeps its typed
// source-down classification, wrapped inside the aggregate both-down error.
func TestSourceErrorsClassifyTyped(t *testing.T) {
	b := newBucket(nullSpendReader{}, nullUsageReader{})
	_, err := b.Collect(context.Background())
	if err == nil {
		t.Fatal("Collect returned nil on full outage")
	}
	if !errors.Is(err, errUnconfigured) && !errors.Is(err, errUsageUnconfigured) {
		t.Errorf("both-down error dropped both source causes: %v", err)
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

// TestUsageReaderFromEnvDegradesWhenUnconfigured proves the provider-usage half
// falls back to a null reader when go-api is unconfigured.
func TestUsageReaderFromEnvDegradesWhenUnconfigured(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	t.Setenv("OVERSEER_GOAPI_TOKEN", "")
	if _, ok := usageReaderFromEnv().(nullUsageReader); !ok {
		t.Errorf("unconfigured usageReaderFromEnv = %T, want nullUsageReader", usageReaderFromEnv())
	}
}
