// Package domainquality is the Overseer's Domain-quality bucket: at a glance, is
// the core product actually good right now — is search returning quality results,
// and is acquisition succeeding? It mirrors two operator reads on go-api's public
// surface through the read-only goapi client:
//
//   - Eval meter (/admin/eval): the in-process eval score against a baseline.
//     go-api already scores it; the Overseer reads the verdict, it never re-runs
//     the pipeline.
//   - Acquisition health (/admin/acquisition): the aggregate success rate,
//     succeeded/(succeeded+failed).
//
// Each read is independent: when one source read is unreachable the bucket serves
// that side's last-known value flagged STALE rather than going dark, and a
// working read on the other side is untouched. History is kept in a bounded ring
// so memory is capped by construction no matter how long the service runs. The
// bucket owns all its own files and self-registers with one blank import in the
// composition root (the additive-buckets invariant).
package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// historyCapacity bounds the retained domain-quality samples. The ring caps
// memory by construction no matter how long the service runs.
const historyCapacity = 120

// discoTrendCapacity bounds the retained discography contamination-trend samples.
// The trend is its own ring — separate from the eval/acquisition history so a
// discography sample never displaces an anchor signal — and is likewise capped by
// construction, so however long the service runs the trend cannot grow without
// limit.
const discoTrendCapacity = 120

// discoDefaultGrouping is the seam default (by= absent) go-api applies; the bucket
// reads it through the bare-path AdminDiscographyQuality and the other groupings
// through the query-capable pivot read.
const discoDefaultGrouping = "artist"

// errUnconfigured is the transport error the null client reports when go-api is
// not configured: both reads render stale rather than the whole service failing
// at startup.
var errUnconfigured = errors.New("domainquality: go-api not configured")

// errBothDown is returned by Collect when both source reads are unreachable and
// nothing fresh arrived, so the shell logs it and skips the store while Render
// keeps serving last-known values flagged stale.
var errBothDown = errors.New("domainquality: eval and acquisition both unreachable")

// reader is the seam onto the two operator reads the bucket mirrors. Depending on
// this interface (not the concrete *goapi.Client) lets a test drive both reads
// deterministically, including one-up-one-down to prove the independent degrade.
type reader interface {
	AdminEval(ctx context.Context) (goapi.EvalStatus, error)
	AdminAcquisition(ctx context.Context) (goapi.AcquisitionStatus, error)
	AdminDiscographyQuality(ctx context.Context) (goapi.DiscographyQuality, error)
	AdminDiscographyQualityBy(ctx context.Context, by string) (goapi.DiscographyQuality, error)
}

// Bucket mirrors go-api's eval-meter score and acquisition success rate into a
// bounded ring and renders them, each side independently flagged stale when its
// read is currently unreachable.
type Bucket struct {
	reader  reader
	history core.Store
	// discoTrend is the bounded ring of top-contamination-ratio samples over time,
	// kept separate from history so a discography sample never displaces an
	// eval/acquisition anchor signal. Capped by construction like history.
	discoTrend core.Store

	// mu guards the last-known eval/acquisition snapshots and their stale flags,
	// which the collect loop writes and the HTTP render reads.
	mu        sync.RWMutex
	lastEval  *goapi.EvalStatus
	evalStale bool
	lastAcq   *goapi.AcquisitionStatus
	acqStale  bool
	// lastDisco is the default (by=artist) worst-first view; discoStale is the
	// block-level stale flag driven by that primary read. discoPivots holds the
	// last-known snapshot per non-default grouping (by=provider|contamination_band),
	// each a genuine re-read of the endpoint that rides the block-level stale flag.
	lastDisco   *goapi.DiscographyQuality
	discoStale  bool
	discoPivots map[string]*goapi.DiscographyQuality
}

// New builds the Domain-quality bucket from the environment. When go-api is not
// configured it falls back to a null client: the bucket still registers and both
// reads render stale rather than crashing.
func New() *Bucket { return newBucket(readerFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// reader; production goes through New.
func newBucket(r reader) *Bucket {
	return &Bucket{
		reader:      r,
		history:     core.NewRingStore(historyCapacity),
		discoTrend:  core.NewRingStore(discoTrendCapacity),
		discoPivots: make(map[string]*goapi.DiscographyQuality),
	}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "domainquality", Title: "Domain quality"}
}

// Collect mirrors both operator reads. Each side records fresh on success or is
// flagged stale on failure while its last-known value is preserved — the two are
// independent, so an eval read failing never disturbs a working acquisition read.
// Only when BOTH reads are unreachable does Collect return an error, so the shell
// logs a genuine outage but never suppresses a half-live panel.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	var signals []core.Signal

	eval, evalErr := b.reader.AdminEval(ctx)
	if evalErr != nil {
		everMirrored := b.markEvalStale()
		b.logSourceUnreachable(ctx, "eval", "GET /admin/eval", everMirrored, evalErr)
	} else {
		b.recordEval(eval)
		signals = append(signals, evalSignal(eval))
	}

	acq, acqErr := b.reader.AdminAcquisition(ctx)
	if acqErr != nil {
		everMirrored := b.markAcqStale()
		b.logSourceUnreachable(ctx, "acquisition", "GET /admin/acquisition", everMirrored, acqErr)
	} else {
		b.recordAcq(acq)
		signals = append(signals, acqSignal(acq))
	}

	// The discography structural-quality read degrades independently like the
	// others: the default (by=artist) worst-first read drives the block-level stale
	// flag, so the endpoint going down flips only the Discography block STALE while
	// eval and acquisition stay live. On a live read the top-contamination ratio is
	// folded into its own bounded trend ring (kept off the eval/acquisition anchor
	// stream) and the on-demand pivots are re-read under their own by= grouping.
	disco, discoErr := b.reader.AdminDiscographyQuality(ctx)
	if discoErr != nil {
		everMirrored := b.markDiscoStale()
		b.logSourceUnreachable(ctx, "discography", "GET /admin/quality/discography", everMirrored, discoErr)
	} else {
		b.recordDisco(disco)
		b.recordDiscoTrend(disco)
		b.collectDiscoPivots(ctx)
	}

	if evalErr != nil && acqErr != nil {
		return nil, fmt.Errorf("%w: eval=%s acquisition=%s", errBothDown, evalErr.Error(), acqErr.Error())
	}
	return signals, nil
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.history.Add(s)
	}
}

// Render builds the panel from the last-known eval and acquisition snapshots
// (each flagged stale if its read is currently unreachable) and the bounded
// history.
func (b *Bucket) Render() core.Panel {
	b.mu.RLock()
	eval, evalStale := b.lastEval, b.evalStale
	acq, acqStale := b.lastAcq, b.acqStale
	disco, discoStale := b.lastDisco, b.discoStale
	pivots := b.snapshotDiscoPivots()
	b.mu.RUnlock()

	history := b.history.Snapshot()
	trend := b.discoTrend.Snapshot()
	return core.Panel{
		Title: b.Meta().Title,
		Body:  renderBody(eval, evalStale, acq, acqStale, disco, discoStale, pivots, trend, history),
	}
}

// recordEval stores the latest eval status and clears its stale flag. The
// snapshot is copied to the heap and only ever replaced (never mutated in place),
// so Render may read the pointer under the lock and use it after.
func (b *Bucket) recordEval(e goapi.EvalStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastEval = &e
	b.evalStale = false
}

// recordAcq stores the latest acquisition snapshot and clears its stale flag,
// with the same replace-never-mutate discipline as recordEval.
func (b *Bucket) recordAcq(a goapi.AcquisitionStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastAcq = &a
	b.acqStale = false
}

// recordDisco stores the latest discography-quality snapshot and clears its stale
// flag, with the same replace-never-mutate discipline as recordEval.
func (b *Bucket) recordDisco(d goapi.DiscographyQuality) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastDisco = &d
	b.discoStale = false
}

// collectDiscoPivots re-reads the endpoint under each non-default grouping so the
// owner's group-on-demand pivot is served live (a genuine ?by=provider /
// ?by=contamination_band read, not a render-side regroup). Each pivot is
// best-effort: a transient failure on one grouping keeps its last-known snapshot
// and rides the block-level STALE driven by the primary read, so a flaky pivot
// never blanks the panel. The reader stays thin — it stores whatever cases the
// endpoint returns for that grouping and never recomputes the verdict.
func (b *Bucket) collectDiscoPivots(ctx context.Context) {
	for _, by := range goapi.DiscographyGroupings {
		if by == discoDefaultGrouping {
			continue // the default view is already read on the bare path
		}
		p, err := b.reader.AdminDiscographyQualityBy(ctx, by)
		if err != nil {
			continue
		}
		b.recordDiscoPivot(by, p)
	}
}

// recordDiscoPivot stores the last-known snapshot for one non-default grouping,
// copied to the heap and replaced (never mutated in place) so Render may read the
// pointer under the lock and use it after.
func (b *Bucket) recordDiscoPivot(by string, d goapi.DiscographyQuality) {
	b.mu.Lock()
	defer b.mu.Unlock()
	snapshot := d
	b.discoPivots[by] = &snapshot
}

// snapshotDiscoPivots copies the per-grouping last-known map under the caller's
// read lock so Render iterates a stable snapshot without holding mu across the
// render. Callers must hold at least the read lock.
func (b *Bucket) snapshotDiscoPivots() map[string]*goapi.DiscographyQuality {
	out := make(map[string]*goapi.DiscographyQuality, len(b.discoPivots))
	for by, p := range b.discoPivots {
		out[by] = p
	}
	return out
}

// recordDiscoTrend folds the top contamination ratio of the served worst-first
// cases into the bounded trend ring. The ratio is pure arithmetic over the served
// counts (contamination suspects / releases) — the reader renders the served
// verdict, it never recomputes disagreement. A response with no rateable case adds
// nothing, so the trend only ever holds real samples.
func (b *Bucket) recordDiscoTrend(d goapi.DiscographyQuality) {
	if sig, ok := discoTrendSignal(d); ok {
		b.discoTrend.Add(sig)
	}
}

// markDiscoStale flags the discography side stale while preserving its last-known
// value, reporting whether the side has ever mirrored a value.
func (b *Bucket) markDiscoStale() (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.discoStale = true
	return b.lastDisco != nil
}

// markEvalStale flags the eval side stale while preserving its last-known value —
// degrade-don't-crash: serve last-known flagged stale rather than dropping it. It
// reports whether the side has ever mirrored a value, so a source that has never
// once succeeded can be surfaced distinctly rather than failing invisibly.
func (b *Bucket) markEvalStale() (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evalStale = true
	return b.lastEval != nil
}

// markAcqStale flags the acquisition side stale while preserving its last-known
// value, reporting whether the side has ever mirrored a value.
func (b *Bucket) markAcqStale() (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acqStale = true
	return b.lastAcq != nil
}

// logSourceUnreachable emits an observability signal when one source read fails.
// The shell only logs overseer.collect.failed when BOTH reads are down, so a
// per-endpoint break — especially one that has never once succeeded — is
// otherwise invisible to operators while the other side stays live. never_mirrored
// distinguishes a source that has failed on every collect since startup (no
// last-known value to show) from one currently stale over a preserved value.
func (b *Bucket) logSourceUnreachable(ctx context.Context, source, op string, everMirrored bool, err error) {
	slog.WarnContext(ctx, "domainquality.source.unreachable",
		"source", source,
		"op", op,
		"never_mirrored", !everMirrored,
		"error", err.Error(),
	)
}

// evalSignal renders one eval status into the shared signal shape for the bounded
// history. The score/state text is watched-app data stored raw; it is HTML-escaped
// at render time.
func evalSignal(e goapi.EvalStatus) core.Signal {
	at := time.Now().UTC()
	if e.LastRun != nil && !e.LastRun.IsZero() {
		at = *e.LastRun
	}
	text := "eval " + e.State
	if e.Scored() {
		text = fmt.Sprintf("eval score=%.2f (%s)", *e.Score, e.State)
	}
	return core.Signal{At: at, Kind: "eval", Text: text}
}

// acqSignal renders one acquisition snapshot into the shared signal shape. The
// rate text is watched-app data stored raw; it is HTML-escaped at render time.
func acqSignal(a goapi.AcquisitionStatus) core.Signal {
	text := "acquisition rate n/a (no completed jobs)"
	if rate, ok := a.SuccessRate(); ok {
		text = fmt.Sprintf("acquisition rate=%.0f%% (ok=%d fail=%d)", rate*100, a.Succeeded, a.Failed)
	}
	return core.Signal{At: time.Now().UTC(), Kind: "acquisition", Text: text}
}

// discoTrendSignal derives one trend sample: the top contamination ratio across
// the served cases (max of contamination-suspects / releases), with the worst
// case's identity for context. It reports ok=false when no case is rateable (no
// cases, or every case has zero releases) so the ring only ever holds real
// samples. The identity is watched-app data stored raw in Text and HTML-escaped at
// render time, never trusted as markup.
func discoTrendSignal(d goapi.DiscographyQuality) (core.Signal, bool) {
	var (
		best   goapi.DiscographyCase
		bestR  float64
		found  bool
		at     = time.Now().UTC()
		latest time.Time
	)
	for _, c := range d.Cases {
		if c.Releases <= 0 {
			continue
		}
		ratio := float64(c.SingleProvider) / float64(c.Releases)
		if !found || ratio > bestR {
			best, bestR, found = c, ratio, true
		}
		if c.LastSeen.After(latest) {
			latest = c.LastSeen
		}
	}
	if !found {
		return core.Signal{}, false
	}
	if !latest.IsZero() {
		at = latest
	}
	label := best.Artist
	if label == "" {
		label = best.ArtistRef
	}
	text := fmt.Sprintf("top contamination %.0f%% — %s (%d/%d suspects)",
		bestR*100, label, best.SingleProvider, best.Releases)
	return core.Signal{At: at, Kind: "discography", Text: text}, true
}

// readerFromEnv builds the read-only goapi client from OVERSEER_GOAPI_URL and the
// process-wide operator token source. Missing or invalid config yields a null
// client so an unconfigured bucket degrades to source-down instead of failing the
// whole service at startup. Config normally lives in the config package, which
// this leaf may not edit; reading the go-api URL here and taking the credential
// from goapi.SharedTokenSource keeps the change within the bucket, matching the
// Reliability and Usage buckets.
func readerFromEnv() reader {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullReader{}
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		// Degrade to source-down, but say why: without this a URL typo is
		// indistinguishable from go-api being genuinely down (a permanently-STALE
		// panel with no diagnostic).
		slog.Warn("domainquality: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return nullReader{}
	}
	return c
}

// nullReader stands in when go-api is unconfigured: both reads report
// source-down, so an unconfigured bucket renders stale rather than nil-panicking.
type nullReader struct{}

func (nullReader) AdminEval(context.Context) (goapi.EvalStatus, error) {
	return goapi.EvalStatus{}, &goapi.SourceDownError{Op: "GET /admin/eval", Err: errUnconfigured}
}

func (nullReader) AdminAcquisition(context.Context) (goapi.AcquisitionStatus, error) {
	return goapi.AcquisitionStatus{}, &goapi.SourceDownError{Op: "GET /admin/acquisition", Err: errUnconfigured}
}

func (nullReader) AdminDiscographyQuality(context.Context) (goapi.DiscographyQuality, error) {
	return goapi.DiscographyQuality{}, &goapi.SourceDownError{Op: "GET /admin/quality/discography", Err: errUnconfigured}
}

func (nullReader) AdminDiscographyQualityBy(_ context.Context, by string) (goapi.DiscographyQuality, error) {
	return goapi.DiscographyQuality{}, &goapi.SourceDownError{Op: "GET /admin/quality/discography?by=" + by, Err: errUnconfigured}
}

func init() { core.Register(New()) }
