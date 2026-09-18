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

// discoTrendCapacity bounds the retained discography contamination-trend samples.
// The trend is its own ring — separate from the eval/acquisition history so a
// discography sample never displaces an anchor signal — and is likewise capped by
// construction, so however long the service runs the trend cannot grow without
// limit.
const discoTrendCapacity = 120

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
}

// Bucket mirrors go-api's eval-meter score and acquisition success rate into a
// bounded ring and renders them, each side independently flagged stale when its
// read is currently unreachable.
type Bucket struct {
	reader reader
	// discoTrend is the bounded ring of top-contamination-ratio samples over time.
	// Capped by construction no matter how long the service runs.
	discoTrend core.Store

	// mu guards the last-known eval/acquisition snapshots and their stale flags,
	// which the collect loop writes and the HTTP render reads.
	mu        sync.RWMutex
	lastEval  *goapi.EvalStatus
	evalStale bool
	lastAcq   *goapi.AcquisitionStatus
	acqStale  bool
	// lastDisco is the default (by=artist) worst-first view; discoStale is the
	// block-level stale flag driven by that primary read.
	lastDisco  *goapi.DiscographyQuality
	discoStale bool
	updated    time.Time
}

// New builds the Domain-quality bucket from the environment. When go-api is not
// configured it falls back to a null client: the bucket still registers and both
// reads render stale rather than crashing.
func New() *Bucket { return newBucket(readerFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// reader; production goes through New.
func newBucket(r reader) *Bucket {
	return &Bucket{
		reader:     r,
		discoTrend: core.NewRingStore(discoTrendCapacity),
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
	eval, evalErr := b.reader.AdminEval(ctx)
	if evalErr != nil {
		everMirrored := b.markEvalStale()
		b.logSourceUnreachable(ctx, "eval", "GET /admin/eval", everMirrored, evalErr)
	} else {
		b.recordEval(eval)
	}

	acq, acqErr := b.reader.AdminAcquisition(ctx)
	if acqErr != nil {
		everMirrored := b.markAcqStale()
		b.logSourceUnreachable(ctx, "acquisition", "GET /admin/acquisition", everMirrored, acqErr)
	} else {
		b.recordAcq(acq)
	}

	// The discography structural-quality read degrades independently like the
	// others: the default (by=artist) worst-first read drives the block-level stale
	// flag, so the endpoint going down flips only the Discography block STALE while
	// eval and acquisition stay live. On a live read the top-contamination ratio is
	// folded into its own bounded trend ring.
	disco, discoErr := b.reader.AdminDiscographyQuality(ctx)
	if discoErr != nil {
		everMirrored := b.markDiscoStale()
		b.logSourceUnreachable(ctx, "discography", "GET /admin/quality/discography", everMirrored, discoErr)
	} else {
		b.recordDisco(disco)
		b.recordDiscoTrend(disco)
	}

	if evalErr != nil && acqErr != nil {
		return nil, fmt.Errorf("%w: eval=%s acquisition=%s", errBothDown, evalErr.Error(), acqErr.Error())
	}
	return nil, nil
}

// Store satisfies the bucket contract. Domain-quality keeps no cross-source anchor
// history; the discography contamination trend is folded in during Collect, so
// there is nothing to persist here.
func (b *Bucket) Store([]core.Signal) {}

// Data is the domain-quality panel payload: the eval meter, acquisition health,
// and discography structural-quality (with its contamination trend), each with its
// independent stale flag. Artist and query strings are watched-app data carried
// raw; React escapes them.
type Data struct {
	Eval        *goapi.EvalStatus         `json:"eval"`
	EvalStale   bool                      `json:"evalStale"`
	Acquisition *goapi.AcquisitionStatus  `json:"acquisition"`
	AcqStale    bool                      `json:"acqStale"`
	Discography *goapi.DiscographyQuality `json:"discography"`
	DiscoStale  bool                      `json:"discoStale"`
	DiscoTrend  []core.Signal             `json:"discoTrend"`
}

// Snapshot builds the domain-quality envelope. The two anchor reads (eval,
// acquisition) drive the state: both unreachable is source_down, either stale or
// not-yet-mirrored is stale, both fresh is live. UpdatedAt is the more recent of
// the two anchors' last successful read.
func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	eval, evalStale := b.lastEval, b.evalStale
	acq, acqStale := b.lastAcq, b.acqStale
	disco, discoStale := b.lastDisco, b.discoStale
	updated := b.updated
	b.mu.RUnlock()

	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     domainState(eval, evalStale, acq, acqStale),
		UpdatedAt: updated,
		Data: core.MarshalData(Data{
			Eval:        eval,
			EvalStale:   evalStale,
			Acquisition: acq,
			AcqStale:    acqStale,
			Discography: disco,
			DiscoStale:  discoStale,
			DiscoTrend:  b.discoTrend.Snapshot(),
		}),
	}
}

// domainState derives the panel state from the two anchor reads, which degrade
// independently: both unreachable is source_down, either stale or not-yet-mirrored
// is stale, both fresh is live.
func domainState(eval *goapi.EvalStatus, evalStale bool, acq *goapi.AcquisitionStatus, acqStale bool) core.State {
	evalDown := evalStale || eval == nil
	acqDown := acqStale || acq == nil
	switch {
	case evalStale && acqStale:
		return core.StateSourceDown
	case evalDown || acqDown:
		return core.StateStale
	default:
		return core.StateLive
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
	b.updated = time.Now().UTC()
}

// recordAcq stores the latest acquisition snapshot and clears its stale flag,
// with the same replace-never-mutate discipline as recordEval.
func (b *Bucket) recordAcq(a goapi.AcquisitionStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastAcq = &a
	b.acqStale = false
	b.updated = time.Now().UTC()
}

// recordDisco stores the latest discography-quality snapshot and clears its stale
// flag, with the same replace-never-mutate discipline as recordEval.
func (b *Bucket) recordDisco(d goapi.DiscographyQuality) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastDisco = &d
	b.discoStale = false
}

// recordDiscoTrend folds the top no-id suspect ratio of the served worst-first
// cases into the bounded trend ring. The ratio is pure arithmetic over the served
// counts (single-provider-without-a-shared-id / releases) — the reader renders the
// served verdict, it never recomputes disagreement. A response with no rateable
// case adds nothing, so the trend only ever holds real samples.
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

// discoTrendSignal derives one trend sample: the top no-id suspect ratio across
// the served cases (max of single-provider-without-a-shared-id / releases — the
// id-anchored suspect measure go-api ranks on), with the worst case's identity and
// id-backing evidence for context. It reports ok=false when no case is rateable (no
// cases, or every case has zero releases) so the ring only ever holds real
// samples. The reader renders the served verdict, it never recomputes disagreement.
// The identity is watched-app data stored raw in Text and HTML-escaped at render
// time, never trusted as markup.
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
		ratio := float64(c.SingleProviderNoID) / float64(c.Releases)
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
	text := fmt.Sprintf("top no-id suspects %.0f%% — %s (%d/%d single-provider, %d without a shared id)",
		bestR*100, label, best.SingleProvider, best.Releases, best.SingleProviderNoID)
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

func init() { core.Register(New()) }
