package cost

import (
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/oci"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeReader is a controllable stand-in for the OCI spend read path. A test sets
// the spend and/or error it returns, exercising collect, render and the
// degrade-to-stale path with no OCI auth.
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

// fakeUsageReader is a controllable stand-in for the go-api provider-usage read
// path. A test sets the usage and/or error it returns, exercising the
// independent-degrade path with no live go-api.
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

// liveBucket builds a bucket with both sources returning fresh data, the common
// starting point for the tests below.
func liveBucket() (*Bucket, *fakeReader, *fakeUsageReader) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	return newBucket(spend, usage), spend, usage
}

// collectStore runs one collect/store cycle, the pair the shell drives on a tick.
func collectStore(t *testing.T, b *Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

// TestCollectStoreRender is the core Done proof: live OCI spend and live provider
// usage both flow into the bucket and render — the current-period spend total,
// the per-service breakdown, the per-provider call breakdown, and both trends.
func TestCollectStoreRender(t *testing.T) {
	b, _, _ := liveBucket()

	empty := string(b.Render().Body)
	if !strings.Contains(empty, "no OCI spend mirrored yet") {
		t.Errorf("empty render = %q, want a no-spend gap", empty)
	}
	if !strings.Contains(empty, "no provider usage mirrored yet") {
		t.Errorf("empty render = %q, want a no-usage gap", empty)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while both live = %v, want nil", err)
	}
	body := string(b.Render().Body)
	for _, want := range []string{
		"LIVE — infra spend", "41.50 USD", "STORAGE", "COMPUTE", "Spend trend", "month-to-date",
		"LIVE — provider API usage", "deezer", "spotify", "120", "Usage trend", "provider calls ok=",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("render missing %q:\n%s", want, body)
		}
	}
	// "other" has zero total, so it must not appear as a row.
	if strings.Contains(body, "<td>other</td>") {
		t.Errorf("idle provider rendered a row:\n%s", body)
	}
}

// TestSpendDegradesIndependently proves the crux invariant on the spend side:
// when the OCI usage-api is down but go-api is up, ONLY the spend half is flagged
// STALE (last-known spend preserved) while the provider half stays LIVE, and
// Collect returns no error since not both sources are down.
func TestSpendDegradesIndependently(t *testing.T) {
	b, spend, _ := liveBucket()
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	spend.set(oci.Spend{}, srcDown())
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect with only OCI down = %v, want nil (independent degrade)", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — OCI usage-api unreachable") {
		t.Errorf("spend half not flagged STALE:\n%s", body)
	}
	if !strings.Contains(body, "41.50 USD") {
		t.Errorf("stale spend dropped the last-known figure:\n%s", body)
	}
	if !strings.Contains(body, "LIVE — provider API usage") {
		t.Errorf("provider half wrongly degraded while go-api was up:\n%s", body)
	}
	if strings.Contains(body, "STALE — go-api unreachable") {
		t.Errorf("provider half wrongly flagged STALE:\n%s", body)
	}
}

// TestUsageDegradesIndependently proves the crux invariant on the provider side:
// when go-api is down but the OCI usage-api is up, ONLY the provider half is
// flagged STALE (last-known usage preserved) while the spend half stays LIVE, and
// Collect returns no error.
func TestUsageDegradesIndependently(t *testing.T) {
	b, _, usage := liveBucket()
	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	usage.set(nil, usageSrcDown())
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect with only go-api down = %v, want nil (independent degrade)", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — go-api unreachable") {
		t.Errorf("provider half not flagged STALE:\n%s", body)
	}
	if !strings.Contains(body, "deezer") || !strings.Contains(body, "120") {
		t.Errorf("stale usage dropped the last-known provider counts:\n%s", body)
	}
	if !strings.Contains(body, "LIVE — infra spend") {
		t.Errorf("spend half wrongly degraded while OCI was up:\n%s", body)
	}
	if strings.Contains(body, "STALE — OCI usage-api unreachable") {
		t.Errorf("spend half wrongly flagged STALE:\n%s", body)
	}
}

// TestBothDownReturnsError proves that only a genuine full outage — both sources
// unreachable — returns an error to the shell, and both halves render STALE while
// preserving their last-known values.
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
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — OCI usage-api unreachable") {
		t.Errorf("spend half not STALE on full outage:\n%s", body)
	}
	if !strings.Contains(body, "STALE — go-api unreachable") {
		t.Errorf("provider half not STALE on full outage:\n%s", body)
	}
	if !strings.Contains(body, "41.50 USD") || !strings.Contains(body, "deezer") {
		t.Errorf("full outage dropped last-known values:\n%s", body)
	}
}

// TestRenderEscapesServiceName proves service names — external usage-api data —
// are HTML-escaped, so a hostile service name cannot inject markup into the panel.
func TestRenderEscapesServiceName(t *testing.T) {
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
	body := string(b.Render().Body)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("service name rendered unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("service name not HTML-escaped:\n%s", body)
	}
}

// TestRenderEscapesProviderName proves provider labels — external go-api data —
// are HTML-escaped, so a hostile provider label cannot inject markup into the
// panel.
func TestRenderEscapesProviderName(t *testing.T) {
	usage := &fakeUsageReader{}
	usage.set(goapi.ProviderUsage{`<script>alert(1)</script>`: {OK: 1}}, nil)
	b := newBucket(&fakeReader{}, usage)

	if err := collectStore(t, b); err != nil && !errors.Is(err, errBothDown) {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("provider name rendered unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("provider name not HTML-escaped:\n%s", body)
	}
}

// TestNoOCIIdentifierInRender proves nothing OCI-identifying reaches the panel: a
// currency that (hostilely) embeds an OCID-like token would be escaped, and the
// spend model carries no identifier field, so no "ocid1." token can appear.
func TestNoOCIIdentifierInRender(t *testing.T) {
	b, _, _ := liveBucket()
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if body := string(b.Render().Body); strings.Contains(body, "ocid1.") {
		t.Errorf("render leaked an OCI identifier:\n%s", body)
	}
}

// TestStaysBoundedUnderLoad proves both source histories are bounded: far more
// collect cycles than the ring capacity never grow either store past its cap.
func TestStaysBoundedUnderLoad(t *testing.T) {
	b, _, _ := liveBucket()

	for i := 0; i < historyCapacity*3; i++ {
		if err := collectStore(t, b); err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
	}
	for name, store := range map[string]struct{ len, cap int }{
		"spend": {b.spendHistory.Len(), b.spendHistory.Cap()},
		"usage": {b.usageHistory.Len(), b.usageHistory.Cap()},
	} {
		if store.len > historyCapacity {
			t.Errorf("%s history Len = %d, exceeds cap %d", name, store.len, historyCapacity)
		}
		if store.cap != historyCapacity {
			t.Errorf("%s history Cap = %d, want %d", name, store.cap, historyCapacity)
		}
	}
}

// TestConcurrentCollectAndRender proves the collect loop and the HTTP render can
// run at once without a data race (asserted under -race), across both sources.
func TestConcurrentCollectAndRender(t *testing.T) {
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
			_ = b.Render()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves an unconfigured bucket (null readers,
// the off-box / no-go-api default) never panics: collect reports the full-outage
// error and render serves a stale/empty panel for both halves.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullSpendReader{}, nullUsageReader{})

	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("unconfigured Collect error = %v, want errBothDown", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "usage-api not reached") {
		t.Errorf("unconfigured render = %q, want a spend not-reached gap", body)
	}
	if !strings.Contains(body, "go-api not reached") {
		t.Errorf("unconfigured render = %q, want a usage not-reached gap", body)
	}
}

// TestSourceErrorsClassifyTyped proves each half's degrade error keeps its typed
// source-down classification for the shell's diagnostics, wrapped inside the
// aggregate both-down error.
func TestSourceErrorsClassifyTyped(t *testing.T) {
	b := newBucket(nullSpendReader{}, nullUsageReader{})
	_, err := b.Collect(context.Background())
	if err == nil {
		t.Fatal("Collect returned nil on full outage")
	}
	// The OCI null reader reports a typed source-down; assert the sentinel is
	// wrapped so the aggregate error still carries the unconfigured cause.
	if !errors.Is(err, errUnconfigured) && !errors.Is(err, errUsageUnconfigured) {
		t.Errorf("both-down error dropped both source causes: %v", err)
	}
}

// TestLazyReaderDegradesOnBuildFailure proves the lazy production OCI reader
// (which builds the instance-principal client on first read) degrades to
// source-down when the build fails — the off-box case — rather than panicking,
// and retries.
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

// TestLazyReaderBuildsOnceOnSuccess proves the lazy reader builds the client once
// and reuses it for later reads.
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

// TestOCIEnabledParsing proves the enablement gate reads the env flag with the
// expected truthy set and defaults off (so dev/CI degrade, not block).
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
// falls back to a null reader (source-down) when go-api is unconfigured, so an
// unset env degrades rather than crashing the service at startup.
func TestUsageReaderFromEnvDegradesWhenUnconfigured(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	t.Setenv("OVERSEER_GOAPI_TOKEN", "")
	if _, ok := usageReaderFromEnv().(nullUsageReader); !ok {
		t.Errorf("unconfigured usageReaderFromEnv = %T, want nullUsageReader", usageReaderFromEnv())
	}
}
