package domainquality

import (
	"altune/overseer/internal/goapi"
	"bytes"
	"context"
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
	// discoByPivot supplies a per-grouping response for the pivot read; a grouping
	// absent here falls back to disco/discoErr, so existing tests that only set
	// disco see every grouping mirror the primary read.
	discoByPivot map[string]goapi.DiscographyQuality
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

func (f fakeReader) AdminDiscographyQualityBy(_ context.Context, by string) (goapi.DiscographyQuality, error) {
	if d, ok := f.discoByPivot[by]; ok {
		return d, nil
	}
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
// NOT error because one side is still fresh. The eval side has never once
// succeeded, so it renders the distinct "unreachable, never mirrored" state (not
// the ambiguous nothing-yet empty state) — a source failing from startup is
// visibly degraded, while the live acquisition side is untouched.
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
	if !strings.Contains(body, "STALE — eval unreachable, never mirrored") {
		t.Fatalf("never-succeeded eval side should show the distinct unreachable state:\n%s", body)
	}
	if strings.Contains(body, "no eval score mirrored yet") {
		t.Fatalf("failing eval side must not show the ambiguous nothing-yet state:\n%s", body)
	}
	if strings.Contains(body, "STALE — acquisition") {
		t.Fatalf("acquisition wrongly flagged stale while live:\n%s", body)
	}
	if !strings.Contains(body, "success rate 95%") {
		t.Fatalf("live acquisition rate missing:\n%s", body)
	}
}

// TestNeverSucceededSourceIsVisiblyDegraded is the #1377 regression: a source that
// 404s on every collect from process start — never a good read, so no last-known
// value — must be visibly degraded, both in the panel (distinct "unreachable,
// never mirrored" state, NOT the "not polled yet" empty state) and in the operator
// log (a per-side signal even though the other side is live and Collect does not
// error). Before the fix the failing side was invisible at every layer.
func TestNeverSucceededSourceIsVisiblyDegraded(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Acquisition 404s on every collect from startup; eval stays live.
	b := newBucket(fakeReader{
		eval:   scoredEval(),
		acqErr: &goapi.SourceDownError{Op: "GET /admin/acquisition", Err: errors.New("404")},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though eval was live: %v", err)
	}

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — acquisition unreachable, never mirrored") {
		t.Fatalf("never-succeeded acquisition side should show the distinct unreachable state:\n%s", body)
	}
	if strings.Contains(body, "no acquisition health mirrored yet") {
		t.Fatalf("failing acquisition side must not show the ambiguous nothing-yet state:\n%s", body)
	}
	if !strings.Contains(body, "score 0.81") {
		t.Fatalf("live eval side missing:\n%s", body)
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "domainquality.source.unreachable") {
		t.Fatalf("expected an operator log for the failing source, got:\n%s", logs)
	}
	if !strings.Contains(logs, `"source":"acquisition"`) {
		t.Fatalf("log should name the failing acquisition source:\n%s", logs)
	}
	if !strings.Contains(logs, `"never_mirrored":true`) {
		t.Fatalf("log should flag the source as never mirrored:\n%s", logs)
	}
}

// TestStaleWithLastKnownStillSurfacesAfterSuccess proves the fix does not disturb
// the already-succeeded path: once a side reads good and then goes down, the panel
// keeps showing the last-known value flagged STALE (not the never-mirrored state).
func TestStaleWithLastKnownStillSurfacesAfterSuccess(t *testing.T) {
	reader := &togglingReader{fakeReader: fakeReader{eval: scoredEval(), acq: healthyAcq()}}
	b := newBucket(reader)
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	reader.down = true
	if _, err := b.Collect(context.Background()); !errors.Is(err, errBothDown) {
		t.Fatalf("second Collect err = %v, want errBothDown", err)
	}

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — eval unreachable, showing last-known score") {
		t.Fatalf("succeeded-then-down eval should show last-known STALE, not never-mirrored:\n%s", body)
	}
	if !strings.Contains(body, "STALE — acquisition unreachable, showing last-known rate") {
		t.Fatalf("succeeded-then-down acquisition should show last-known STALE, not never-mirrored:\n%s", body)
	}
	if strings.Contains(body, "never mirrored") {
		t.Fatalf("a side with a last-known value must not render the never-mirrored state:\n%s", body)
	}
	if !strings.Contains(body, "score 0.81") || !strings.Contains(body, "success rate 95%") {
		t.Fatalf("last-known values dropped under stale:\n%s", body)
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

// TestRendersDiscographyCase is the tracer's Done proof for the reader half: a
// fresh collect renders the discography case with its provider split in the
// panel — the owner can see one real discography flagged.
func TestRendersDiscographyCase(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "spotify:4Z8W4fKeB5YxbusRsdQVPb") {
		t.Fatalf("panel missing the discography case:\n%s", body)
	}
	if !strings.Contains(body, "42 releases") || !strings.Contains(body, "9 contamination suspect(s)") {
		t.Fatalf("panel missing release/suspect counts:\n%s", body)
	}
	// Provider split rendered in a stable sorted order.
	if !strings.Contains(body, "musicbrainz:12 spotify:40") {
		t.Fatalf("panel missing sorted provider split:\n%s", body)
	}
}

// TestDiscographyIsHintNotVerdict pins the core rule: a case is framed as a
// "suspect" with its provider evidence, never asserted as "wrong".
func TestDiscographyIsHintNotVerdict(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "suspect") {
		t.Fatalf("case should be framed as a suspect:\n%s", body)
	}
	if strings.Contains(strings.ToLower(body), "wrong") {
		t.Fatalf("disagreement must be a hint, never asserted as wrong:\n%s", body)
	}
}

// TestDiscographyRenderEscapes proves a hostile artist ref / provider name cannot
// inject markup into the panel.
func TestDiscographyRenderEscapes(t *testing.T) {
	evil := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "artist",
		Cases: []goapi.DiscographyCase{{
			Artist:         `<script>alert('x')</script>`,
			ArtistRef:      `<img src=x onerror=alert(1)>`,
			Releases:       3,
			SingleProvider: 1,
			ProviderCounts: map[string]int{`<b>evil</b>`: 2},
		}},
	}
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: evil})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img src=x") || strings.Contains(body, "<b>evil</b>") {
		t.Fatalf("discography watched-app text was not HTML-escaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped entities in discography block:\n%s", body)
	}
}

// TestDiscographyIndependentDegrade proves the discography read degrades on its
// own: down while eval + acquisition stay live flags only the discography block
// STALE and never errors Collect.
func TestDiscographyIndependentDegrade(t *testing.T) {
	b := newBucket(fakeReader{
		eval:     scoredEval(),
		acq:      healthyAcq(),
		discoErr: &goapi.SourceDownError{Op: "GET /admin/quality/discography", Err: errors.New("boom")},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though eval + acquisition were live: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — discography quality unreachable, never mirrored") {
		t.Fatalf("failing discography side should show the distinct unreachable state:\n%s", body)
	}
	if !strings.Contains(body, "score 0.81") || !strings.Contains(body, "success rate 95%") {
		t.Fatalf("live eval/acquisition sides disturbed by the discography failure:\n%s", body)
	}
}

// TestDiscographyProviderEvidence proves each row carries provider-by-provider
// evidence: the per-provider split, the single-provider suspect count, and the
// incompleteness gap between the widest and narrowest provider.
func TestDiscographyProviderEvidence(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "provider evidence: musicbrainz:12 spotify:40") {
		t.Fatalf("missing per-provider evidence line:\n%s", body)
	}
	if !strings.Contains(body, "9 single-provider suspect release(s)") {
		t.Fatalf("missing single-provider suspect callout:\n%s", body)
	}
	if !strings.Contains(body, "incompleteness gap: spotify lists 40 vs musicbrainz lists 12 (gap 28)") {
		t.Fatalf("missing incompleteness gap:\n%s", body)
	}
}

// TestDiscographySingleProviderSuspectCalledOut proves a case whose releases all
// come from one provider is called out as a single-provider suspect (the strongest
// contamination hint), framed as a suspect and never as wrong.
func TestDiscographySingleProviderSuspectCalledOut(t *testing.T) {
	disco := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "artist",
		Cases: []goapi.DiscographyCase{{
			Artist: "Obscure Act", ArtistRef: "deezer:99",
			Releases: 5, SingleProvider: 5,
			ProviderCounts: map[string]int{"deezer": 5},
		}},
	}
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: disco})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "single-provider suspect: only deezer listed these releases") {
		t.Fatalf("single-provider case not called out:\n%s", body)
	}
	if strings.Contains(strings.ToLower(body), "wrong") {
		t.Fatalf("evidence must be a hint, never asserted wrong:\n%s", body)
	}
}

// TestDiscographyPivotRegroups proves the group-on-demand pivot: the endpoint is
// re-read under each grouping (by=provider|contamination_band) and the panel shows
// the regrouped case list, not a render-side re-sort of the artist view.
func TestDiscographyPivotRegroups(t *testing.T) {
	byProvider := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "provider",
		Cases: []goapi.DiscographyCase{{
			Artist: "musicbrainz", Releases: 120, SingleProvider: 0,
			ProviderCounts: map[string]int{"musicbrainz": 120},
		}},
	}
	byBand := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "contamination_band",
		Cases: []goapi.DiscographyCase{{
			Artist: "high (>50%)", Releases: 7, SingleProvider: 6,
			ProviderCounts: map[string]int{"spotify": 7},
		}},
	}
	b := newBucket(fakeReader{
		eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco(),
		discoByPivot: map[string]goapi.DiscographyQuality{
			"provider":           byProvider,
			"contamination_band": byBand,
		},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "Pivot (group on demand):") {
		t.Fatalf("missing pivot control:\n%s", body)
	}
	if !strings.Contains(body, "grouped by provider") || !strings.Contains(body, "provider evidence: musicbrainz:120") {
		t.Fatalf("provider pivot did not regroup:\n%s", body)
	}
	if !strings.Contains(body, "grouped by contamination_band") || !strings.Contains(body, "high (&gt;50%)") {
		t.Fatalf("contamination_band pivot did not regroup (and escape):\n%s", body)
	}
}

// TestDiscographyTrendBoundedAndRendered proves the top-contamination-ratio trend
// is rendered and its ring is bounded by construction across many collect cycles.
func TestDiscographyTrendBoundedAndRendered(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: radioheadDisco()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	body := string(b.Render().Body)
	// Radiohead: 9 suspects / 42 releases = 21%.
	if !strings.Contains(body, "contamination trend (top ratio over time):") || !strings.Contains(body, "top contamination 21%") {
		t.Fatalf("missing rendered contamination trend:\n%s", body)
	}
	for i := 0; i < discoTrendCapacity*3; i++ {
		if _, err := b.Collect(context.Background()); err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
	}
	if got := b.discoTrend.Len(); got != discoTrendCapacity {
		t.Fatalf("trend Len = %d, want capped at %d", got, discoTrendCapacity)
	}
	if got := b.discoTrend.Cap(); got != discoTrendCapacity {
		t.Fatalf("trend Cap = %d, want %d", got, discoTrendCapacity)
	}
}

// TestDiscographyStaleIndependentDegradeGoodThenDown proves the STALE
// independent-degrade: after a good read the endpoint goes down, the Discography
// block flips STALE with its last-known cases while eval and acquisition stay live.
func TestDiscographyStaleIndependentDegradeGoodThenDown(t *testing.T) {
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

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE — discography quality unreachable, showing last-known cases") {
		t.Fatalf("discography not flagged STALE with last-known:\n%s", body)
	}
	if !strings.Contains(body, "spotify:4Z8W4fKeB5YxbusRsdQVPb") {
		t.Fatalf("last-known discography cases dropped under STALE:\n%s", body)
	}
	if strings.Contains(body, "STALE — eval") || strings.Contains(body, "STALE — acquisition") {
		t.Fatalf("eval/acquisition wrongly flagged STALE while live:\n%s", body)
	}
	if !strings.Contains(body, "score 0.81") || !strings.Contains(body, "success rate 95%") {
		t.Fatalf("live eval/acquisition disturbed by the discography outage:\n%s", body)
	}
}

// TestDiscographyTrendAndPivotEscaped proves the trend line and the regrouped pivot
// rows also HTML-escape watched-app strings, closing the escaping gap the new
// render surfaces open (not just the primary case list).
func TestDiscographyTrendAndPivotEscaped(t *testing.T) {
	evil := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "artist",
		Cases: []goapi.DiscographyCase{{
			Artist:   `<script>alert('trend')</script>`,
			Releases: 4, SingleProvider: 4,
			ProviderCounts: map[string]int{"deezer": 4},
		}},
	}
	pivotEvil := goapi.DiscographyQuality{
		WindowDays: 30, GroupBy: "provider",
		Cases: []goapi.DiscographyCase{{
			Artist: `<img src=x onerror=alert(2)>`, Releases: 4, SingleProvider: 0,
			ProviderCounts: map[string]int{`<b>evil</b>`: 4},
		}},
	}
	b := newBucket(fakeReader{
		eval: scoredEval(), acq: healthyAcq(), disco: evil,
		discoByPivot: map[string]goapi.DiscographyQuality{"provider": pivotEvil},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img src=x") || strings.Contains(body, "<b>evil</b>") {
		t.Fatalf("trend/pivot watched-app text was not HTML-escaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&lt;img") {
		t.Fatalf("expected escaped entities in trend/pivot:\n%s", body)
	}
}

// discoTogglingReader flips only the discography reads to source-down when
// discoDown is set, so the block can be driven good-then-down while eval and
// acquisition stay live.
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

func (r *discoTogglingReader) AdminDiscographyQualityBy(ctx context.Context, by string) (goapi.DiscographyQuality, error) {
	if r.discoDown {
		return goapi.DiscographyQuality{}, &goapi.SourceDownError{Op: "GET /admin/quality/discography?by=" + by, Err: errors.New("down")}
	}
	return r.fakeReader.AdminDiscographyQualityBy(ctx, by)
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
