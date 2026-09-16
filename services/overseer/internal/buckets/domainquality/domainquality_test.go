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
		evalErr: &goapi.SourceDownError{Op: "GET /admin/eval", Err: errors.New("boom")},
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
		acqErr: &goapi.SourceDownError{Op: "GET /admin/acquisition", Err: errors.New("404")},
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
		Cases: []goapi.DiscographyCase{{
			Artist: "Radiohead", ArtistRef: "spotify:4Z8W4fKeB5YxbusRsdQVPb",
			Releases: 42, SingleProvider: 9,
			ProviderCounts: map[string]int{"spotify": 40, "musicbrainz": 12},
		}},
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
	if c.ProviderCounts["spotify"] != 40 || c.ProviderCounts["musicbrainz"] != 12 {
		t.Fatalf("provider split not carried: %+v", c.ProviderCounts)
	}
}

// TestDiscographyIndependentDegrade proves the discography read degrades on its own.
func TestDiscographyIndependentDegrade(t *testing.T) {
	b := newBucket(fakeReader{
		eval:     scoredEval(),
		acq:      healthyAcq(),
		discoErr: &goapi.SourceDownError{Op: "GET /admin/quality/discography", Err: errors.New("boom")},
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
		return goapi.DiscographyQuality{}, &goapi.SourceDownError{Op: "GET /admin/quality/discography", Err: errors.New("down")}
	}
	return r.fakeReader.AdminDiscographyQuality(ctx)
}

// togglingReader flips both anchor reads to source-down when down is set.
type togglingReader struct {
	fakeReader
	down bool
}

func (r *togglingReader) AdminEval(ctx context.Context) (goapi.EvalStatus, error) {
	if r.down {
		return goapi.EvalStatus{}, &goapi.SourceDownError{Op: "GET /admin/eval", Err: errors.New("down")}
	}
	return r.fakeReader.AdminEval(ctx)
}

func (r *togglingReader) AdminAcquisition(ctx context.Context) (goapi.AcquisitionStatus, error) {
	if r.down {
		return goapi.AcquisitionStatus{}, &goapi.SourceDownError{Op: "GET /admin/acquisition", Err: errors.New("down")}
	}
	return r.fakeReader.AdminAcquisition(ctx)
}
