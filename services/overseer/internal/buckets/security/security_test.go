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

func TestSuiteFailsWhenProbeServed(t *testing.T) {
	res := runSuite(context.Background(), staticProber{status: 200}, defaultSuite(), time.Now)
	if res.passed() != 0 {
		t.Errorf("checks passed on a 200 (served) response: %d, want 0", res.passed())
	}
}

func TestSuiteFailsOnServerError(t *testing.T) {
	res := runSuite(context.Background(), staticProber{status: 500}, defaultSuite(), time.Now)
	if res.passed() != 0 {
		t.Errorf("checks passed on a 500 response: %d, want 0", res.passed())
	}
}

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

func TestDegradeToStale(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), staticProber{status: 401}, defaultSuite(), time.Now))

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

func TestBoundedHistory(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	for i := 0; i < 4*historyCapacity; i++ {
		b.record(runSuite(context.Background(), staticProber{status: 401}, defaultSuite(), time.Now))
	}
	if got := b.history.Len(); got != historyCapacity {
		t.Errorf("retained %d history entries, want capped at %d", got, historyCapacity)
	}
}

func TestStartKeepsSelfTestFiringPastOneRefresh(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b.Start(ctx)

	deadline := time.After(2 * time.Second)
	for b.history.Len() < 2 {
		select {
		case <-deadline:
			t.Fatalf("scheduler recorded %d refreshes through Start; it froze after one — the #1812 regression", b.history.Len())
		case <-time.After(time.Millisecond):
		}
	}
}

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

func TestSeverityCriticalWhenASelfTestFails(t *testing.T) {
	b := newBucket(staticProber{status: 200}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), b.scheduler.client, b.scheduler.checks, time.Now))

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — a failed self-test is not a stale source", snap.State)
	}
	if snap.Headline != "regression detected — 4 of 4 self-tests failing" {
		t.Errorf("headline = %q, want the regression verdict", snap.Headline)
	}
}

func TestSeverityOKWhenEveryDefenseHolds(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), b.scheduler.client, b.scheduler.checks, time.Now))

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "all defenses held — 4/4 self-tests passed" {
		t.Errorf("headline = %q, want the all-clear verdict", snap.Headline)
	}
}

func TestSeverityWarnsBeforeTheFirstRun(t *testing.T) {
	snap := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour).Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if snap.Headline != "no self-test run yet" {
		t.Errorf("headline = %q, want the unproven marker", snap.Headline)
	}
}

func TestUnconfiguredDegrades(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	if _, ok := clientFromEnv().(nullProber); !ok {
		t.Error("unconfigured clientFromEnv did not return a null prober")
	}
	b := newBucket(nullProber{}, defaultSuite(), time.Hour)
	b.record(runSuite(context.Background(), nullProber{}, defaultSuite(), time.Now))

	snap := b.Snapshot()
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
