package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeReader drives both operator reads deterministically, including one-up-one-
// down so the independent degrade can be proven.
type fakeReader struct {
	eval     goapi.EvalStatus
	evalErr  error
	acq      goapi.AcquisitionStatus
	acqErr   error
	disco    goapi.DiscographyQuality
	discoErr error
}

func (f fakeReader) AdminEval(context.Context) (goapi.EvalStatus, error) {
	return f.eval, f.evalErr
}

func (f fakeReader) AdminAcquisition(context.Context) (goapi.AcquisitionStatus, error) {
	return f.acq, f.acqErr
}

func (f fakeReader) AdminDiscographyQuality(context.Context) (goapi.DiscographyQuality, error) {
	return f.disco, f.discoErr
}

func ptr(f float64) *float64 { return &f }

func scoredEval() goapi.EvalStatus {
	return goapi.EvalStatus{
		Enabled: true, State: "ok", Score: ptr(0.81), Baseline: ptr(0.75),
		Queries: []goapi.EvalQuery{{Query: "miles davis", Passed: true}},
	}
}

func healthyAcq() goapi.AcquisitionStatus {
	return goapi.AcquisitionStatus{Succeeded: 19, Failed: 1, InFlight: 2, QueueDepth: 4, QueueCapacity: 64}
}

func snapData(t *testing.T, snap core.Snapshot) Data {
	t.Helper()
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

// TestSnapshotScoreAndRate is the core Done proof: a fresh collect carries the eval
// score/baseline and the acquisition counters in the snapshot payload.
func TestSnapshotScoreAndRate(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if d.Eval == nil || d.Eval.Score == nil || *d.Eval.Score != 0.81 {
		t.Fatalf("eval score not carried: %+v", d.Eval)
	}
	if d.Acquisition == nil || d.Acquisition.Succeeded != 19 || d.Acquisition.Failed != 1 {
		t.Fatalf("acquisition counters not carried: %+v", d.Acquisition)
	}
}

// TestDegradeToSourceDownPreservesLastKnown: both sources down flips the panel
// source_down while keeping the last-known values flagged stale.
func TestDegradeToSourceDownPreservesLastKnown(t *testing.T) {
	reader := &togglingReader{fakeReader: fakeReader{eval: scoredEval(), acq: healthyAcq()}}
	b := newBucket(reader)

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	reader.down = true
	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("second Collect err = %v, want errBothDown", err)
	}

	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("state = %q, want source_down", snap.State)
	}
	d := snapData(t, snap)
	if !d.EvalStale || !d.AcqStale {
		t.Fatalf("both sides not stale: eval=%v acq=%v", d.EvalStale, d.AcqStale)
	}
	if d.Eval == nil || d.Acquisition == nil {
		t.Fatalf("last-known values dropped under stale: %+v", d)
	}
}

// TestIndependentDegrade: eval down while acquisition stays live flags only eval
// stale, and Collect does NOT error because one side is still fresh.
func TestIndependentDegrade(t *testing.T) {
	b := newBucket(fakeReader{
		evalErr: &goapi.SourceDownError{Op: "GET /observe/eval", Err: errors.New("boom")},
		acq:     healthyAcq(),
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though acquisition was live: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if !d.EvalStale {
		t.Fatal("eval side not flagged stale")
	}
	if d.Eval != nil {
		t.Fatalf("never-mirrored eval side carried a value: %+v", d.Eval)
	}
	if d.AcqStale {
		t.Fatal("acquisition wrongly flagged stale while live")
	}
	if d.Acquisition == nil || d.Acquisition.Succeeded != 19 {
		t.Fatalf("live acquisition missing: %+v", d.Acquisition)
	}
}

// TestNeverSucceededSourceIsLogged is the #1377 regression: a source that fails on
// every collect from process start is logged per-side even while the other side is
// live and Collect does not error.
func TestNeverSucceededSourceIsLogged(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	b := newBucket(fakeReader{
		eval:   scoredEval(),
		acqErr: &goapi.SourceDownError{Op: "GET /observe/acquisition", Err: errors.New("404")},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though eval was live: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if !d.AcqStale || d.Acquisition != nil {
		t.Fatalf("never-mirrored acquisition should be stale with no value: %+v", d)
	}
	logs := logBuf.String()
	if !strings.Contains(logs, "domainquality.source.unreachable") ||
		!strings.Contains(logs, `"source":"acquisition"`) ||
		!strings.Contains(logs, `"never_mirrored":true`) {
		t.Fatalf("expected a never-mirrored operator log for acquisition, got:\n%s", logs)
	}
}

// TestSnapshotCarriesRawText proves a hostile go-api response is carried verbatim
// in the payload (React escapes it on render).
func TestSnapshotCarriesRawText(t *testing.T) {
	evil := goapi.EvalStatus{
		Enabled: true, State: "ok", Score: ptr(0.5), Baseline: ptr(0.5),
		Error:   `<script>alert('err')</script>`,
		Queries: []goapi.EvalQuery{{Query: `<img src=x onerror=alert(1)>`, Passed: false}},
	}
	b := newBucket(fakeReader{eval: evil, acq: healthyAcq()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if d.Eval == nil || d.Eval.Error != `<script>alert('err')</script>` {
		t.Fatalf("eval error not carried verbatim: %+v", d.Eval)
	}
	if len(d.Eval.Queries) != 1 || d.Eval.Queries[0].Query != `<img src=x onerror=alert(1)>` {
		t.Fatalf("eval query not carried verbatim: %+v", d.Eval)
	}
}

// TestMetaIsStable pins the bucket identity the registry and shell rely on.
func TestMetaIsStable(t *testing.T) {
	m := newBucket(nullReader{}).Meta()
	if m.ID != "domainquality" || m.Title != "Domain quality" {
		t.Fatalf("Meta = %+v, want id=domainquality title=Domain quality", m)
	}
}

// TestUnconfiguredDegradesNotCrash proves the null reader degrades cleanly.
func TestUnconfiguredDegradesNotCrash(t *testing.T) {
	b := New()
	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("unconfigured Collect err = %v, want errBothDown", err)
	}
	snap := b.Snapshot() // must not panic
	if snap.Title != "Domain quality" || snap.State != core.StateSourceDown {
		t.Fatalf("snapshot = %+v, want title 'Domain quality' state source_down", snap)
	}
}

func radioheadDisco() goapi.DiscographyQuality {
	return goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "artist",
		SuspectRate:  0.375,
		LastSampleAt: time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC),
		Cases: []goapi.DiscographyCase{{
			Artist: "Radiohead", ArtistRef: "spotify:4Z8W4fKeB5YxbusRsdQVPb",
			Releases: 42, SingleProvider: 9, SingleProviderNoID: 4,
			ProviderCounts: map[string]int{"spotify": 40, "musicbrainz": 12},
		}},
	}
}

// TestSuspectRateRidesIntoSnapshot proves the served windowed suspect-rate headline
// and its last-sample time are carried verbatim into the snapshot payload — the
// bucket renders go-api's served number, it never recomputes it.
func TestSuspectRateRidesIntoSnapshot(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if d.Discography == nil {
		t.Fatal("discography missing from snapshot")
	}
	if d.Discography.SuspectRate != 0.375 {
		t.Fatalf("suspect_rate = %v, want 0.375 (the served headline, carried verbatim)", d.Discography.SuspectRate)
	}
	want := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	if !d.Discography.LastSampleAt.Equal(want) {
		t.Fatalf("last_sample_at = %v, want %v", d.Discography.LastSampleAt, want)
	}
}

// TestDiscographyCaseInSnapshot proves the discography case, with its provider
// split, is carried in the payload verbatim.
func TestDiscographyCaseInSnapshot(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if d.Discography == nil || len(d.Discography.Cases) != 1 {
		t.Fatalf("discography case missing: %+v", d.Discography)
	}
	c := d.Discography.Cases[0]
	if c.ArtistRef != "spotify:4Z8W4fKeB5YxbusRsdQVPb" || c.Releases != 42 || c.SingleProvider != 9 {
		t.Fatalf("discography case not carried verbatim: %+v", c)
	}
	// The id-backing evidence must ride through to the panel row verbatim: of the 9
	// single-provider releases, 4 lack a shared id — the real suspects.
	if c.SingleProviderNoID != 4 {
		t.Fatalf("id-backing evidence not carried: single_provider_no_id = %d, want 4", c.SingleProviderNoID)
	}
	if c.ProviderCounts["spotify"] != 40 || c.ProviderCounts["musicbrainz"] != 12 {
		t.Fatalf("provider split not carried: %+v", c.ProviderCounts)
	}
}

// TestDiscoTrendSignalIDAnchoredAndEvidenced plants the id-anchor render rule on
// the trend headline: the worst case is picked by the no-id suspect ratio, not raw
// headcount, and the rendered text carries the id-backing evidence. An id-verified
// single-provider artist (headcount ratio 1.0 but zero no-id suspects) is NOT
// chosen over a genuine no-id suspect with a lower headcount ratio.
func TestDiscoTrendSignalIDAnchoredAndEvidenced(t *testing.T) {
	d := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "artist",
		Cases: []goapi.DiscographyCase{
			// Every single-provider release is id-verified: no real suspects.
			{Artist: "IdVerified", ArtistRef: "a", Releases: 10, SingleProvider: 10, SingleProviderNoID: 0},
			// Lower headcount ratio, but the single-provider releases carry no id.
			{Artist: "NoId", ArtistRef: "b", Releases: 10, SingleProvider: 3, SingleProviderNoID: 3},
		},
	}
	sig, ok := discoTrendSignal(d)
	if !ok {
		t.Fatal("discoTrendSignal reported no rateable case, want the no-id suspect")
	}
	if !strings.Contains(sig.Text, "NoId") {
		t.Fatalf("trend picked %q, want the no-id artist (id anchor, not headcount)", sig.Text)
	}
	if !strings.Contains(sig.Text, "without a shared id") {
		t.Fatalf("trend text %q carries no id-backing evidence", sig.Text)
	}
}

// TestDiscographyIndependentDegrade proves the discography read degrades on its own.
func TestDiscographyIndependentDegrade(t *testing.T) {
	b := newBucket(fakeReader{
		eval:     scoredEval(),
		acq:      healthyAcq(),
		discoErr: &goapi.SourceDownError{Op: "GET /observe/quality/discography", Err: errors.New("boom")},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though eval + acquisition were live: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if !d.DiscoStale || d.Discography != nil {
		t.Fatalf("never-mirrored discography should be stale with no value: %+v", d)
	}
	if d.Eval == nil || d.Acquisition == nil {
		t.Fatalf("live eval/acquisition disturbed by the discography failure: %+v", d)
	}
}

// TestDiscographyTrendBounded proves the top-contamination-ratio trend is carried
// and its ring is bounded across many collect cycles.
func TestDiscographyTrendBounded(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if len(snapData(t, b.Snapshot()).DiscoTrend) == 0 {
		t.Fatal("contamination trend empty, want a sample")
	}
	for i := 0; i < discoTrendCapacity*3; i++ {
		if _, err := b.Collect(context.Background()); err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
	}
	if got := b.discoTrend.Len(); got != discoTrendCapacity {
		t.Fatalf("trend Len = %d, want capped at %d", got, discoTrendCapacity)
	}
}

// TestDiscographyStaleGoodThenDown proves the STALE independent-degrade: after a
// good read the endpoint goes down; the discography half flips stale with its
// last-known cases while eval and acquisition stay live.
func TestDiscographyStaleGoodThenDown(t *testing.T) {
	reader := &discoTogglingReader{fakeReader: fakeReader{
		eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco(),
	}}
	b := newBucket(reader)
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	reader.discoDown = true
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though eval + acquisition were live: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if !d.DiscoStale {
		t.Fatal("discography not flagged stale")
	}
	if d.Discography == nil || len(d.Discography.Cases) != 1 {
		t.Fatalf("last-known discography cases dropped under stale: %+v", d.Discography)
	}
	if d.EvalStale || d.AcqStale {
		t.Fatalf("eval/acquisition wrongly flagged stale while live: %+v", d)
	}
}

// discoTogglingReader flips only the discography reads to source-down.
type discoTogglingReader struct {
	fakeReader
	discoDown bool
}

func (r *discoTogglingReader) AdminDiscographyQuality(ctx context.Context) (goapi.DiscographyQuality, error) {
	if r.discoDown {
		return goapi.DiscographyQuality{}, &goapi.SourceDownError{Op: "GET /observe/quality/discography", Err: errors.New("down")}
	}
	return r.fakeReader.AdminDiscographyQuality(ctx)
}

// TestSeverityCriticalWhenSuspectRateHigh is the health-grade proof: every read
// is fresh and live, but go-api reports most discography opens firing a suspect —
// the product is bad right now, so the bucket grades itself critical and the
// headline is the number that says so.
func TestSeverityCriticalWhenSuspectRateHigh(t *testing.T) {
	b := newBucket(fakeReader{
		eval:  scoredEval(),
		acq:   healthyAcq(),
		disco: goapi.DiscographyQuality{SuspectRate: 0.62},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — a bad payload is not a stale source", snap.State)
	}
	if snap.Headline != "suspect rate 62%" {
		t.Errorf("headline = %q, want the suspect rate that drove the grade", snap.Headline)
	}
}

// TestSeverityCriticalWhenAcquisitionFailing proves the second gradeable read
// stands on its own: search quality is fine and nothing is suspect, but most
// acquisitions in the recent window are failing. The window is the delta between
// two collects (3 succeeded, 17 failed since the baseline), so the grade reads
// the recent spike, not a lifetime average.
func TestSeverityCriticalWhenAcquisitionFailing(t *testing.T) {
	reader := &steppingAcqReader{
		fakeReader: fakeReader{eval: scoredEval(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}},
		acqs: []goapi.AcquisitionStatus{
			{Succeeded: 500, Failed: 20},
			{Succeeded: 503, Failed: 37},
		},
	}
	b := newBucket(reader)
	collectN(t, b, 2)

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.Headline != "acquisition success 15%" {
		t.Errorf("headline = %q, want the recent-window acquisition rate that drove the grade", snap.Headline)
	}
}

// TestAcquisitionRateReflectsRecentWindow is the ticket's headline proof: a
// lifetime-healthy loop (99% succeeded) that just started failing grades critical
// on the recent window, and the served AcqWindow rate is ~0 — an all-time ratio
// stayed green through the same spike.
func TestAcquisitionRateReflectsRecentWindow(t *testing.T) {
	reader := &steppingAcqReader{
		fakeReader: fakeReader{eval: scoredEval(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}},
		acqs: []goapi.AcquisitionStatus{
			{Succeeded: 990, Failed: 10},  // lifetime ~99%
			{Succeeded: 990, Failed: 110}, // 100 recent failures, none succeeded
		},
	}
	b := newBucket(reader)
	collectN(t, b, 2)

	snap := b.Snapshot()
	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical — the recent window is all failures", snap.Severity)
	}
	d := snapData(t, snap)
	if d.AcqWindow == nil {
		t.Fatal("AcqWindow absent though two samples spanned 100 recent completions")
	}
	if d.AcqWindow.Rate != 0 || d.AcqWindow.Completed != 100 {
		t.Fatalf("AcqWindow = %+v, want rate 0 over 100 recent completions", d.AcqWindow)
	}
}

// TestAcquisitionWindowUndefinedOnFirstCollect proves the window needs two samples:
// a single collect (only a lifetime cumulative baseline) yields no windowed rate,
// so the measure is skipped rather than read as a spurious 0%.
func TestAcquisitionWindowUndefinedOnFirstCollect(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if d.AcqWindow != nil {
		t.Fatalf("AcqWindow present after a single collect: %+v", d.AcqWindow)
	}
}

// TestEvalScoreFlaggedStaleByAge proves a score go-api last computed long ago is
// flagged stale even while the read that fetched it is perfectly reachable.
func TestEvalScoreFlaggedStaleByAge(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	ranLongAgo := now.Add(-5 * 24 * time.Hour)
	stale := scoredEval()
	stale.LastRun = &ranLongAgo

	b := newBucket(fakeReader{eval: stale, acq: healthyAcq()})
	b.now = func() time.Time { return now }
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if !d.EvalAgeStale {
		t.Fatal("a 5-day-old eval score was not flagged stale by age")
	}
	if d.EvalStale {
		t.Fatal("age-staleness must not set the read-reachability stale flag")
	}
}

// TestFreshEvalScoreNotFlaggedStaleByAge is the arm that must disagree: a score
// go-api computed moments ago is not age-stale.
func TestFreshEvalScoreNotFlaggedStaleByAge(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	ranJustNow := now.Add(-2 * time.Hour)
	fresh := scoredEval()
	fresh.LastRun = &ranJustNow

	b := newBucket(fakeReader{eval: fresh, acq: healthyAcq()})
	b.now = func() time.Time { return now }
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if snapData(t, b.Snapshot()).EvalAgeStale {
		t.Fatal("a 2h-old eval score was wrongly flagged stale by age")
	}
}

// TestSeverityWarnsWhenEvalBelowBaseline proves a search-quality regression warns
// rather than pages: nothing is down, the results just got worse than the
// baseline go-api scores against.
func TestSeverityWarnsWhenEvalBelowBaseline(t *testing.T) {
	regressed := scoredEval()
	regressed.Score = ptr(0.61)
	b := newBucket(fakeReader{
		eval:  regressed,
		acq:   healthyAcq(),
		disco: goapi.DiscographyQuality{SuspectRate: 0.01},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if snap.Headline != "eval 0.61 vs baseline 0.75" {
		t.Errorf("headline = %q, want the eval score against its baseline", snap.Headline)
	}
}

// TestSeverityOKWhenEveryMeasureHealthy is the arm that has to disagree with the
// three above: healthy payloads grade ok and the headline falls back to the
// bucket's own declared headline number.
func TestSeverityOKWhenEveryMeasureHealthy(t *testing.T) {
	b := newBucket(fakeReader{
		eval:  scoredEval(),
		acq:   healthyAcq(),
		disco: goapi.DiscographyQuality{SuspectRate: 0.01},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "suspect rate 1%" {
		t.Errorf("headline = %q, want the suspect rate", snap.Headline)
	}
}

// TestSeverityIgnoresUnrateableMeasures proves a measure that could not be taken
// never counts as healthy OR as a fault: with no read ever mirrored the bucket
// says so rather than reporting a green all-clear it never measured.
func TestSeverityIgnoresUnrateableMeasures(t *testing.T) {
	snap := newBucket(fakeReader{}).Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "no quality signal yet" {
		t.Errorf("headline = %q, want the no-signal marker", snap.Headline)
	}
}

// togglingReader flips both anchor reads to source-down when down is set.
type togglingReader struct {
	fakeReader
	down bool
}

func (r *togglingReader) AdminEval(ctx context.Context) (goapi.EvalStatus, error) {
	if r.down {
		return goapi.EvalStatus{}, &goapi.SourceDownError{Op: "GET /observe/eval", Err: errors.New("down")}
	}
	return r.fakeReader.AdminEval(ctx)
}

func (r *togglingReader) AdminAcquisition(ctx context.Context) (goapi.AcquisitionStatus, error) {
	if r.down {
		return goapi.AcquisitionStatus{}, &goapi.SourceDownError{Op: "GET /observe/acquisition", Err: errors.New("down")}
	}
	return r.fakeReader.AdminAcquisition(ctx)
}

// steppingAcqReader walks a sequence of acquisition snapshots across successive
// collects, holding the last one, so a test can drive the cumulative counters that
// the windowed success rate reads as a delta.
type steppingAcqReader struct {
	fakeReader
	acqs []goapi.AcquisitionStatus
	i    int
}

func (r *steppingAcqReader) AdminAcquisition(context.Context) (goapi.AcquisitionStatus, error) {
	a := r.acqs[min(r.i, len(r.acqs)-1)]
	r.i++
	return a, nil
}

// collectN drives n collect cycles, failing the test on any error, so a windowed
// assertion can build up the samples it needs.
func collectN(t *testing.T, b *Bucket, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := b.Collect(context.Background()); err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
	}
}
