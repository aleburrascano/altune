package security_test

import (
	"altune/overseer/internal/buckets/security"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	historyCapacity = 120
	suiteSize       = 4
	burstSize       = 8
	maxErrLen       = 200
	searchPath      = "/v1/discovery/search"
	selftestQuery   = "q=overseer-selftest"
)

var epoch = time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("EST", -5*60*60))

type outcome func(path, rawQuery string) (int, error)

func status(code int) outcome {
	return func(string, string) (int, error) { return code, nil }
}

func down(err error) outcome {
	return func(string, string) (int, error) { return 0, err }
}

func panics() outcome {
	return func(string, string) (int, error) { panic("self-test probe blew up") }
}

type script struct {
	mu    sync.Mutex
	runs  []outcome
	n     int
	calls map[string]int
}

func newScript(runs ...outcome) *script {
	return &script{runs: runs, calls: map[string]int{}}
}

func (s *script) now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return runAt(s.n)
}

func runAt(n int) time.Time {
	return epoch.Add(time.Duration(n) * time.Minute)
}

func (s *script) Probe(_ context.Context, path, rawQuery string) (int, error) {
	s.mu.Lock()
	idx := min(s.n, len(s.runs)) - 1
	s.calls[path+"?"+rawQuery]++
	run := s.runs[idx]
	s.mu.Unlock()
	return run(path, rawQuery)
}

func (s *script) started() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func (s *script) callsTo(path, rawQuery string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[path+"?"+rawQuery]
}

func startBucket(t *testing.T, s *script, interval time.Duration) (*security.Bucket, *recordingSeries) {
	t.Helper()
	b := security.New(security.Deps{Prober: s, Interval: interval, Now: s.now})
	series := &recordingSeries{}
	b.UseSeries(series)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.Start(ctx)
	return b, series
}

func runScripted(t *testing.T, runs ...outcome) *security.Bucket {
	t.Helper()
	s := newScript(runs...)
	b, _ := startBucket(t, s, time.Millisecond)
	waitFor(t, func() bool { return s.started() > len(runs) }, "the scripted runs to be recorded")
	return b
}

func runOnce(t *testing.T, run outcome) (*security.Bucket, *script, *recordingSeries) {
	t.Helper()
	s := newScript(run)
	b, series := startBucket(t, s, time.Hour)
	waitFor(t, func() bool {
		snap := b.Snapshot()
		return snapData(t, snap).HasRun || snap.Reason == goapi.ReasonDown
	}, "the first run to be recorded")
	return b, s, series
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func snapData(t *testing.T, snap core.Snapshot) security.Data {
	t.Helper()
	var d security.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func checkNamed(t *testing.T, d security.Data, name string) security.CheckView {
	t.Helper()
	for _, c := range d.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", name, d.Checks)
	return security.CheckView{}
}

func TestSuitePassesWhenAppRejects(t *testing.T) {
	b, _, _ := runOnce(t, status(401))

	snap := b.Snapshot()
	d := snapData(t, snap)
	if d.Passed != suiteSize || d.Total != suiteSize {
		t.Fatalf("passed %d of %d, want all %d checks passing", d.Passed, d.Total, suiteSize)
	}
	if snap.State != core.StateLive || snap.Reason != "" {
		t.Errorf("state = %q reason = %q after a reachable run, want live with no reason", snap.State, snap.Reason)
	}
}

func TestEachCheckIsShownWithItsVerdict(t *testing.T) {
	b, _, _ := runOnce(t, status(429))

	want := []security.CheckView{
		{Name: "unauth-v1", Desc: "unauthenticated /v1 read is rejected", Reached: true, Passed: false, Status: 429},
		{Name: "observe-gate", Desc: "unauthenticated /observe read is rejected", Reached: true, Passed: false, Status: 429},
		{Name: "rate-limit-burst", Desc: "a burst is shed or rejected, never served", Reached: true, Passed: true, Status: 429},
		{Name: "bad-input", Desc: "a malformed read is rejected cleanly, not a 500", Reached: true, Passed: false, Status: 429},
	}
	if got := snapData(t, b.Snapshot()).Checks; !reflect.DeepEqual(got, want) {
		t.Fatalf("checks = %+v, want %+v", got, want)
	}
}

func TestEachCheckAcceptsOnlyItsRejectionCodes(t *testing.T) {
	cases := map[int][]string{
		400: {"bad-input"},
		401: {"unauth-v1", "observe-gate", "rate-limit-burst", "bad-input"},
		403: {"unauth-v1", "observe-gate", "rate-limit-burst", "bad-input"},
		404: {"bad-input"},
		422: {"bad-input"},
		429: {"rate-limit-burst"},
		200: nil,
		302: nil,
		500: nil,
	}
	for code, passing := range cases {
		b, _, _ := runOnce(t, status(code))
		var got []string
		for _, c := range snapData(t, b.Snapshot()).Checks {
			if c.Passed {
				got = append(got, c.Name)
			}
		}
		if !reflect.DeepEqual(got, passing) {
			t.Errorf("on %d the passing checks are %v, want %v", code, got, passing)
		}
	}
}

func TestTheBurstSendsEightProbesAndTheRestOne(t *testing.T) {
	_, s, _ := runOnce(t, status(401))

	if got := s.callsTo(searchPath, selftestQuery); got != burstSize {
		t.Errorf("burst sent %d probes, want %d", got, burstSize)
	}
	for _, p := range []string{"/v1/library?", "/observe/health?", searchPath + "?q=%zz%00&limit=not-a-number"} {
		path, query, _ := strings.Cut(p, "?")
		if got := s.callsTo(path, query); got != 1 {
			t.Errorf("%s sent %d probes, want 1", p, got)
		}
	}
}

func TestABurstStopsAtTheFirstServedProbe(t *testing.T) {
	_, s, _ := runOnce(t, status(200))

	if got := s.callsTo(searchPath, selftestQuery); got != 1 {
		t.Errorf("served burst sent %d probes, want it to stop after 1", got)
	}
}

func TestABurstStopsAtTheFirstUnreachedProbe(t *testing.T) {
	_, s, _ := runOnce(t, down(errors.New("dial tcp: refused")))

	if got := s.callsTo(searchPath, selftestQuery); got != 1 {
		t.Errorf("unreached burst sent %d probes, want it to stop after 1", got)
	}
}

func TestARunIsStampedWithItsStartInUTC(t *testing.T) {
	b, _, _ := runOnce(t, status(401))

	snap := b.Snapshot()
	d := snapData(t, snap)
	want := runAt(1).UTC()
	if !d.LastRun.Equal(want) || d.LastRun.Location() != time.UTC {
		t.Errorf("lastRun = %v, want %v", d.LastRun, want)
	}
	if !snap.UpdatedAt.Equal(want) || snap.UpdatedAt.Location() != time.UTC {
		t.Errorf("updatedAt = %v, want %v", snap.UpdatedAt, want)
	}
	if len(d.History) != 1 || d.History[0].Kind != "selftest" || d.History[0].Text != "security self-test 4/4 passed" || !d.History[0].At.Equal(want) {
		t.Errorf("history = %+v, want one selftest signal for 4/4 at %v", d.History, want)
	}
}

func TestAPartialRunShowsWhatWasNotReached(t *testing.T) {
	exact := strings.Repeat("e", maxErrLen)
	over := strings.Repeat("o", maxErrLen+1)
	b, _, _ := runOnce(t, func(path, _ string) (int, error) {
		switch path {
		case "/observe/health":
			return 0, errors.New(exact)
		case "/v1/library":
			return 0, errors.New(over)
		}
		return 401, nil
	})

	snap := b.Snapshot()
	d := snapData(t, snap)
	if snap.Severity != core.SeverityWarn || snap.Headline != "partial run — 2 of 4 self-tests reached" {
		t.Errorf("severity %q headline %q, want warn and the partial-run verdict", snap.Severity, snap.Headline)
	}
	if d.Passed != 2 || d.Total != suiteSize {
		t.Errorf("passed %d of %d, want 2 of %d", d.Passed, d.Total, suiteSize)
	}
	if c := checkNamed(t, d, "observe-gate"); c.Reached || c.Passed || c.Error != exact {
		t.Errorf("observe-gate = %+v, want unreached with its whole %d-byte error", c, maxErrLen)
	}
	if c := checkNamed(t, d, "unauth-v1"); c.Reached || c.Error != over[:maxErrLen]+"…" {
		t.Errorf("unauth-v1 error = %q, want it cut to %d bytes and marked", c.Error, maxErrLen)
	}
	if c := checkNamed(t, d, "bad-input"); !c.Reached || !c.Passed || c.Error != "" {
		t.Errorf("bad-input = %+v, want reached, passed, no error", c)
	}
}

func TestDegradeToStale(t *testing.T) {
	b := runScripted(t, status(401), down(errors.New("dial tcp: refused")))

	snap := b.Snapshot()
	if snap.State != core.StateSourceDown || snap.Reason != goapi.ReasonDown {
		t.Errorf("state %q reason %q after go-api went unreachable, want source_down / %q", snap.State, snap.Reason, goapi.ReasonDown)
	}
	d := snapData(t, snap)
	if !d.HasRun || d.Passed != suiteSize || !d.LastRun.Equal(runAt(1)) {
		t.Errorf("data = %+v, want the last reachable verdict (4/4 at run 1) kept", d)
	}
}

func TestStaleClearsOnRecovery(t *testing.T) {
	b := runScripted(t, down(errors.New("down")), status(401))

	snap := b.Snapshot()
	if snap.State != core.StateLive || snap.Reason != "" {
		t.Errorf("state %q reason %q after go-api recovered, want live with no reason", snap.State, snap.Reason)
	}
}

func TestBoundedHistory(t *testing.T) {
	s := newScript(status(401))
	b, _ := startBucket(t, s, time.Millisecond)
	waitFor(t, func() bool { return s.started() > historyCapacity+20 }, "more runs than the history holds")

	if got := len(snapData(t, b.Snapshot()).History); got != historyCapacity {
		t.Errorf("retained %d history entries, want capped at %d", got, historyCapacity)
	}
}

func TestStartKeepsSelfTestFiringPastOneRefresh(t *testing.T) {
	s := newScript(status(401))
	b, _ := startBucket(t, s, time.Millisecond)

	waitFor(t, func() bool { return len(snapData(t, b.Snapshot()).History) >= 2 },
		"a second refresh through Start; it froze after one — the #1812 regression")
}

func TestSeverityCriticalWhenASelfTestFails(t *testing.T) {
	b, _, _ := runOnce(t, status(200))

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

func TestOneFailingSelfTestOutranksAnUnreachedOne(t *testing.T) {
	b, _, _ := runOnce(t, func(path, _ string) (int, error) {
		switch path {
		case "/v1/library":
			return 200, nil
		case "/observe/health":
			return 0, errors.New("dial tcp: refused")
		}
		return 401, nil
	})

	snap := b.Snapshot()
	if snap.Severity != core.SeverityCritical || snap.Headline != "regression detected — 1 of 4 self-tests failing" {
		t.Errorf("severity %q headline %q, want critical for the one served probe", snap.Severity, snap.Headline)
	}
}

func TestSeverityOKWhenEveryDefenseHolds(t *testing.T) {
	b, _, _ := runOnce(t, status(401))

	snap := b.Snapshot()
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "all defenses held — 4/4 self-tests passed" {
		t.Errorf("headline = %q, want the all-clear verdict", snap.Headline)
	}
}

func TestSeverityWarnsBeforeTheFirstRun(t *testing.T) {
	snap := security.New(security.Deps{}).Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if snap.Headline != "no self-test run yet" {
		t.Errorf("headline = %q, want the unproven marker", snap.Headline)
	}
	if snap.State != core.StateStale || snapData(t, snap).HasRun {
		t.Errorf("state %q before any run, want stale with no run", snap.State)
	}
}

func TestUnconfiguredDegrades(t *testing.T) {
	b := security.New(security.Deps{Interval: time.Hour})
	series := &recordingSeries{}
	b.UseSeries(series)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.Start(ctx)

	waitFor(t, func() bool { return b.Snapshot().Reason == goapi.ReasonDown }, "the unconfigured run to degrade")
	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
	if snapData(t, snap).HasRun {
		t.Errorf("HasRun = true, want false (no reachable run yet)")
	}
	if got := series.named("probe_failures"); len(got) != 1 || got[0].Value != suiteSize {
		t.Errorf("probe_failures = %+v, want one point of %d", got, suiteSize)
	}
}

func TestCollectAndStoreLeaveTheVerdictAlone(t *testing.T) {
	b, _, _ := runOnce(t, status(401))
	before := b.Snapshot()

	signals, err := b.Collect(context.Background())
	b.Store([]core.Signal{{Kind: "x"}})

	if signals != nil || err != nil {
		t.Errorf("Collect = %v, %v, want nothing: the scheduler drives this bucket", signals, err)
	}
	if after := b.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Errorf("snapshot changed across Collect/Store: %+v -> %+v", before, after)
	}
}

func TestMetaNamesTheBucket(t *testing.T) {
	m := security.New(security.Deps{}).Meta()
	if m.ID != "security" || m.Title != "Security" {
		t.Errorf("meta = %+v, want security / Security", m)
	}
}

func TestARestoredHistoryRingShowsInTheSnapshot(t *testing.T) {
	b := security.New(security.Deps{})
	restored := []core.Signal{{At: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Kind: "suite", Text: "4/4 passed"}}

	ring := b.Rings()["history"]
	if ring == nil {
		t.Fatal("Rings() has no history ring")
	}

	ring.Restore(restored)

	var data security.Data
	if err := json.Unmarshal(b.Snapshot().Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.History) != 1 || data.History[0].Text != "4/4 passed" {
		t.Fatalf("snapshot history = %+v, want the restored %+v", data.History, restored)
	}
}

const (
	bucketID            = "security"
	seriesFindingsOpen  = "findings_open"
	seriesProbeFailures = "probe_failures"
)

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
		if p.bucket == bucketID && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

func TestRecordWritesEachSeriesOncePerRun(t *testing.T) {
	_, _, series := runOnce(t, status(200))

	findings := series.named(seriesFindingsOpen)
	if len(findings) != 1 {
		t.Fatalf("findings_open recorded %d points, want 1: %+v", len(findings), findings)
	}
	if findings[0].Value != suiteSize || !findings[0].At.Equal(runAt(1)) {
		t.Errorf("findings_open = %+v, want %d at the run's start (every check served, none rejected)", findings[0], suiteSize)
	}
	failures := series.named(seriesProbeFailures)
	if len(failures) != 1 {
		t.Fatalf("probe_failures recorded %d points, want 1: %+v", len(failures), failures)
	}
	if failures[0].Value != 0 || !failures[0].At.Equal(runAt(1)) {
		t.Errorf("probe_failures = %+v, want 0 at the run's start (every check reached go-api)", failures[0])
	}
}

func TestRecordCountsUnreachedRunAsProbeFailures(t *testing.T) {
	_, _, series := runOnce(t, down(errors.New("dial tcp: refused")))

	failures := series.named(seriesProbeFailures)
	if len(failures) != 1 || failures[0].Value != suiteSize {
		t.Errorf("probe_failures = %+v, want one point of %d", failures, suiteSize)
	}
	findings := series.named(seriesFindingsOpen)
	if len(findings) != 1 || findings[0].Value != 0 {
		t.Errorf("findings_open = %+v, want one point of 0 (nothing reached to grade)", findings)
	}
}

func TestKeySeriesIsFindingsOpen(t *testing.T) {
	if got := security.New(security.Deps{}).KeySeries(); got != seriesFindingsOpen {
		t.Errorf("KeySeries() = %q, want %q", got, seriesFindingsOpen)
	}
}

func TestSecurityBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = security.New(security.Deps{})
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("security does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}
