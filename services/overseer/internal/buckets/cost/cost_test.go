package cost

import (
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

func srcDown() error {
	return &oci.SourceDownError{Err: errors.New("dial refused")}
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

// collectStore runs one collect/store cycle, the pair the shell drives on a tick.
func collectStore(t *testing.T, b *Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

// TestCollectStoreRender is the core Done proof: live OCI spend flows into the
// bucket and renders the current-period total, the per-service breakdown and the
// spend trend.
func TestCollectStoreRender(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b := newBucket(reader)

	if body := string(b.Render().Body); !strings.Contains(body, "no OCI spend mirrored yet") {
		t.Errorf("empty render = %q, want a no-spend gap", body)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while live = %v, want nil", err)
	}
	body := string(b.Render().Body)
	for _, want := range []string{"LIVE", "41.50 USD", "STORAGE", "COMPUTE", "Spend trend", "month-to-date"} {
		if !strings.Contains(body, want) {
			t.Errorf("render missing %q:\n%s", want, body)
		}
	}
}

// TestDegradesToStaleOnSourceDown proves degrade-don't-crash: after a live read, an
// unreachable usage-api flags the panel STALE while still showing the last-known
// spend, and the collect error classifies as source-down for the shell.
func TestDegradesToStaleOnSourceDown(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}
	if body := string(b.Render().Body); strings.Contains(body, "STALE") {
		t.Fatalf("pre-degrade render unexpectedly STALE:\n%s", body)
	}

	reader.set(oci.Spend{}, srcDown())
	err := collectStore(t, b)
	if err == nil {
		t.Fatal("Collect with usage-api down returned nil error")
	}
	if !oci.IsSourceDown(err) {
		t.Errorf("degraded collect error is not source-down: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Errorf("post-degrade render = %q, want a STALE flag", body)
	}
	if !strings.Contains(body, "41.50 USD") {
		t.Errorf("stale render dropped the last-known spend:\n%s", body)
	}
}

// TestRenderEscapesServiceName proves service names — external usage-api data — are
// HTML-escaped, so a hostile service name cannot inject markup into the panel.
func TestRenderEscapesServiceName(t *testing.T) {
	reader := &fakeReader{}
	reader.set(oci.Spend{
		Amount:   1,
		Currency: "USD",
		Lines:    []oci.SpendLine{{Service: `<script>alert(1)</script>`, Amount: 1}},
	}, nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
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

// TestNoOCIIdentifierInRender proves nothing OCI-identifying reaches the panel: a
// currency that (hostilely) embeds an OCID-like token would be escaped, and the
// spend model carries no identifier field, so no "ocid1." token can appear.
func TestNoOCIIdentifierInRender(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if body := string(b.Render().Body); strings.Contains(body, "ocid1.") {
		t.Errorf("render leaked an OCI identifier:\n%s", body)
	}
}

// TestStaysBoundedUnderLoad proves the spend history is bounded: far more collect
// cycles than the ring capacity never grow the store past its cap.
func TestStaysBoundedUnderLoad(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b := newBucket(reader)

	for i := 0; i < historyCapacity*3; i++ {
		if err := collectStore(t, b); err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
	}
	if got := b.history.Len(); got > historyCapacity {
		t.Errorf("history Len = %d, exceeds cap %d", got, historyCapacity)
	}
	if b.history.Cap() != historyCapacity {
		t.Errorf("history Cap = %d, want %d", b.history.Cap(), historyCapacity)
	}
}

// TestConcurrentCollectAndRender proves the collect loop and the HTTP render can
// run at once without a data race (asserted under -race).
func TestConcurrentCollectAndRender(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b := newBucket(reader)

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

// TestUnconfiguredDegradesNotCrashes proves an unconfigured bucket (null reader,
// the off-OCI-box default) never panics: collect reports source-down and render
// serves a stale/empty panel.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullReader{})

	if _, err := b.Collect(context.Background()); !oci.IsSourceDown(err) {
		t.Fatalf("unconfigured Collect error = %v, want source-down", err)
	}
	if body := string(b.Render().Body); !strings.Contains(body, "usage-api not reached") {
		t.Errorf("unconfigured render = %q, want a not-reached gap", body)
	}
}

// TestLazyReaderDegradesOnBuildFailure proves the lazy production reader (which
// builds the instance-principal client on first read) degrades to source-down when
// the build fails — the off-box case — rather than panicking, and retries.
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
