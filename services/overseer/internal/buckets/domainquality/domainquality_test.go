package domainquality

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeReader drives both operator reads deterministically, including one-up-one-
// down so the independent degrade can be proven.
type fakeReader struct {
	eval    goapi.EvalStatus
	evalErr error
	acq     goapi.AcquisitionStatus
	acqErr  error
}

func (f fakeReader) AdminEval(context.Context) (goapi.EvalStatus, error) {
	return f.eval, f.evalErr
}

func (f fakeReader) AdminAcquisition(context.Context) (goapi.AcquisitionStatus, error) {
	return f.acq, f.acqErr
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

// TestRendersScoreAndRate is the core Done proof: a fresh collect renders the
// eval score vs baseline and the acquisition success rate in the panel.
func TestRendersScoreAndRate(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq()})
	signals, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2 (eval + acquisition)", len(signals))
	}
	b.Store(signals)

	body := string(b.Render().Body)
	if !strings.Contains(body, "score 0.81 vs baseline 0.75") {
		t.Fatalf("panel missing eval score line:\n%s", body)
	}
	if !strings.Contains(body, "success rate 95% (19 ok / 1 failed)") {
		t.Fatalf("panel missing acquisition rate line:\n%s", body)
	}
	if !strings.Contains(body, "2 quality sample(s)") {
		t.Fatalf("panel missing bounded history:\n%s", body)
	}
}

// TestDegradeToStalePreservesLastKnown proves the degrade-don't-crash invariant:
// after a good read, a later unreachable read keeps the last-known values and
// flags them STALE rather than dropping the panel.
func TestDegradeToStalePreservesLastKnown(t *testing.T) {
	reader := &togglingReader{fakeReader: fakeReader{eval: scoredEval(), acq: healthyAcq()}}
	b := newBucket(reader)

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	// Both sources now go down; nothing fresh arrives.
	reader.down = true
	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("second Collect err = %v, want errBothDown", err)
	}

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — eval unreachable") {
		t.Fatalf("eval not flagged stale:\n%s", body)
	}
	if !strings.Contains(body, "STALE — acquisition unreachable") {
		t.Fatalf("acquisition not flagged stale:\n%s", body)
	}
	// Last-known values still shown.
	if !strings.Contains(body, "score 0.81") || !strings.Contains(body, "success rate 95%") {
		t.Fatalf("last-known values dropped under stale:\n%s", body)
	}
}

// TestIndependentDegrade proves the two reads degrade independently: eval down
// while acquisition stays live flags only the eval side stale, and Collect does
// NOT error because one side is still fresh.
func TestIndependentDegrade(t *testing.T) {
	b := newBucket(fakeReader{
		evalErr: &goapi.SourceDownError{Op: "GET /admin/eval", Err: errors.New("boom")},
		acq:     healthyAcq(),
	})
	signals, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect errored though acquisition was live: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1 (acquisition only)", len(signals))
	}
	b.Store(signals)

	body := string(b.Render().Body)
	if !strings.Contains(body, "no eval score mirrored yet") {
		t.Fatalf("eval side should show nothing-yet (never had a good read):\n%s", body)
	}
	if strings.Contains(body, "STALE — acquisition") {
		t.Fatalf("acquisition wrongly flagged stale while live:\n%s", body)
	}
	if !strings.Contains(body, "success rate 95%") {
		t.Fatalf("live acquisition rate missing:\n%s", body)
	}
}

// TestRenderEscapesWatchedAppText proves a hostile go-api response cannot inject
// markup: an eval query and error carrying HTML are escaped in the rendered
// panel.
func TestRenderEscapesWatchedAppText(t *testing.T) {
	evil := goapi.EvalStatus{
		Enabled: true, State: "ok", Score: ptr(0.5), Baseline: ptr(0.5),
		Error:   `<script>alert('err')</script>`,
		Queries: []goapi.EvalQuery{{Query: `<img src=x onerror=alert(1)>`, Passed: false}},
	}
	b := newBucket(fakeReader{eval: evil, acq: healthyAcq()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img src=x") {
		t.Fatalf("watched-app text was not HTML-escaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&lt;img") {
		t.Fatalf("expected escaped entities in panel:\n%s", body)
	}
}

// TestBoundedHistory proves the ring caps memory: far more collect cycles than
// the capacity never grow the store past its bound.
func TestBoundedHistory(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq()})
	for i := 0; i < historyCapacity*3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
		b.Store(signals)
	}
	if got := b.history.Len(); got != historyCapacity {
		t.Fatalf("history Len = %d, want capped at %d", got, historyCapacity)
	}
	if got := b.history.Cap(); got != historyCapacity {
		t.Fatalf("history Cap = %d, want %d", got, historyCapacity)
	}
}

// TestMetaIsStable pins the bucket identity the registry and shell rely on.
func TestMetaIsStable(t *testing.T) {
	m := newBucket(nullReader{}).Meta()
	if m.ID != "domainquality" || m.Title != "Domain quality" {
		t.Fatalf("Meta = %+v, want id=domainquality title=Domain quality", m)
	}
}

// TestUnconfiguredRendersStaleNotCrash proves the null reader degrades cleanly:
// an unconfigured bucket collects an error and renders the nothing-yet state
// rather than nil-panicking.
func TestUnconfiguredRendersStaleNotCrash(t *testing.T) {
	b := New() // no OVERSEER_GOAPI_* env => nullReader
	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("unconfigured Collect err = %v, want errBothDown", err)
	}
	panel := b.Render() // must not panic
	if panel.Title != "Domain quality" {
		t.Fatalf("panel title = %q", panel.Title)
	}
}

// togglingReader flips both reads to source-down when down is set, so a bucket
// can be driven good-then-down within one test.
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
