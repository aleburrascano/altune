package security

import (
	"altune/overseer/internal/core"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"
)

// staticProber drives the suite deterministically: it answers every probe with
// a fixed status, or reports every probe source-down when downErr is set.
type staticProber struct {
	status  int
	downErr error
}

func (staticProber) target(path, rawQuery string) *url.URL {
	return &url.URL{Scheme: "https", Host: "fake.test", Path: path, RawQuery: rawQuery}
}

func (p staticProber) do(context.Context, *url.URL) probeResult {
	if p.downErr != nil {
		return probeResult{err: &sourceDown{err: p.downErr}}
	}
	return probeResult{status: p.status}
}

// TestSuitePassesWhenAppRejects proves the whole suite passes when go-api
// rejects every probe (401): the app is defending itself.
func TestSuitePassesWhenAppRejects(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), b.scheduler.client, b.scheduler.checks, time.Now))

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.last == nil || b.last.passed() != b.last.total() {
		t.Fatalf("want all checks passing, got %+v", b.last)
	}
	if b.stale {
		t.Error("verdict flagged stale after a reachable run")
	}
}

// TestSuiteFailsWhenProbeServed proves a probe that is SERVED (200) fails its
// check: the defense let it through, which is a regression the panel must show.
func TestSuiteFailsWhenProbeServed(t *testing.T) {
	res := runSuite(context.Background(), staticProber{status: 200}, defaultSuite(), time.Now)
	if res.passed() != 0 {
		t.Errorf("checks passed on a 200 (served) response: %d, want 0", res.passed())
	}
}

// TestSuiteFailsOnServerError proves a 500 fails the check: the app fell over
// rather than cleanly rejecting, which is not a pass.
func TestSuiteFailsOnServerError(t *testing.T) {
	res := runSuite(context.Background(), staticProber{status: 500}, defaultSuite(), time.Now)
	if res.passed() != 0 {
		t.Errorf("checks passed on a 500 response: %d, want 0", res.passed())
	}
}

// TestBurstAccepts429 proves the rate-limit check passes when a burst is shed
// with 429 — the defense holding.
func TestBurstAccepts429(t *testing.T) {
	res := runSuite(context.Background(), staticProber{status: 429}, defaultSuite(), time.Now)
	var burst checkResult
	for _, r := range res.results {
		if r.name == "rate-limit-burst" {
			burst = r
		}
	}
	if !burst.passed {
		t.Errorf("rate-limit-burst did not pass on 429: %+v", burst)
	}
}

// TestDegradeToStale is the degrade-don't-crash proof: after a good run, a run
// where go-api is fully unreachable preserves the last-known verdict and flags
// it STALE rather than dropping it.
func TestDegradeToStale(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), staticProber{status: 401}, defaultSuite(), time.Now))

	// Now a fully-down run: no check reaches go-api.
	down := runSuite(context.Background(), staticProber{downErr: errors.New("dial tcp: refused")}, defaultSuite(), time.Now)
	b.record(down)

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.last == nil {
		t.Fatal("last-known verdict was dropped on a down run — should degrade, not go dark")
	}
	if !b.stale {
		t.Error("verdict not flagged stale after go-api went unreachable")
	}
	if b.last.passed() != b.last.total() {
		t.Error("stale verdict was overwritten by the down run")
	}
}

// TestStaleClearsOnRecovery proves the stale flag clears once go-api is
// reachable again.
func TestStaleClearsOnRecovery(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), staticProber{downErr: errors.New("down")}, defaultSuite(), time.Now))
	b.record(runSuite(context.Background(), staticProber{status: 401}, defaultSuite(), time.Now))

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.stale {
		t.Error("stale flag did not clear after go-api recovered")
	}
}

// TestBoundedHistory proves the retained self-test history is capped no matter
// how many runs record — memory is bounded by construction.
func TestBoundedHistory(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	for i := 0; i < 4*historyCapacity; i++ {
		b.record(runSuite(context.Background(), staticProber{status: 401}, defaultSuite(), time.Now))
	}
	if got := b.history.Len(); got != historyCapacity {
		t.Errorf("retained %d history entries, want capped at %d", got, historyCapacity)
	}
}

// TestSelfRegisters proves the bucket self-registers into the Default registry
// from its package init via nothing but the blank import — the additive path.
func TestSelfRegisters(t *testing.T) {
	found := false
	for _, bk := range core.Default.Buckets() {
		if bk.Meta().ID == "security" {
			found = true
		}
	}
	if !found {
		t.Error("security bucket did not self-register into core.Default")
	}
}

// TestUnconfiguredDegrades proves an unconfigured bucket (no go-api URL) builds a
// null prober and reports source_down rather than crashing. A fully-down first run
// leaves no last-known verdict, so the payload's HasRun is false.
func TestUnconfiguredDegrades(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	if _, ok := clientFromEnv().(nullProber); !ok {
		t.Error("unconfigured clientFromEnv did not return a null prober")
	}
	b := newBucket(nullProber{}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), nullProber{}, defaultSuite(), time.Now))

	snap := b.Snapshot() // must not panic
	if snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if d.HasRun {
		t.Errorf("HasRun = true, want false (no reachable run yet)")
	}
}
