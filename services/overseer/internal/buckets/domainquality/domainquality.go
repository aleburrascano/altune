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

const (
	bucketID               = "domainquality"
	seriesDiscoSuccessRate = "disco_success_rate"
	seriesEvalScore        = "eval_score"
	seriesAcquisitionRate  = "acquisition_rate"
)

// The health bands the bucket grades itself on, hoisted from the panel's own
// traffic lights (web/src/panels/domainquality.panel.tsx) so one change moves the
// grade and the colour together. Suspect rate is a contamination measure (higher
// is worse); acquisition success is its inverse (lower is worse).
const (
	warnSuspectRate     = 0.2
	criticalSuspectRate = 0.5
	warnAcqRate         = 0.9
	criticalAcqRate     = 0.7
)

// evalFreshness is how old the eval score may get before the bucket flags it stale
// by age. go-api's eval meter runs every 6h (internal/admin/evalmeter/meter.go), so
// a score older than two scheduled runs means at least one run was missed — stale
// regardless of whether the read that fetched it is reachable.
const evalFreshness = 12 * time.Hour

const (
	perReadTimeoutFallback = 7 * time.Second
	perReadTimeoutMargin   = 500 * time.Millisecond
)

// acqWindow is the recent span the acquisition success rate is measured over, so a
// current failure spike shows even while the lifetime average stays high.
// acqSampleCapacity bounds the retained counter samples so memory is capped by
// construction however long the service runs; it spans the window with room to
// spare at the default 5s tick (internal/config: OVERSEER_TICK_INTERVAL).
const (
	acqWindow         = 10 * time.Minute
	acqSampleCapacity = 300
)

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
	series     core.Series

	// now is the clock, injected so the age-based eval staleness and the windowed
	// acquisition rate are deterministic under test. Production uses the wall clock.
	now func() time.Time

	// mu guards the last-known eval/acquisition snapshots and their stale flags,
	// which the collect loop writes and the HTTP render reads.
	mu         sync.RWMutex
	lastEval   *goapi.EvalStatus
	evalStale  bool
	evalReason string
	lastAcq    *goapi.AcquisitionStatus
	acqStale   bool
	acqReason  string
	// acqSamples is the bounded ring of cumulative acquisition counter
	// observations. The windowed success rate is the delta between the earliest
	// sample still inside acqWindow and the latest, so only recent completions
	// count. Trimmed to acqSampleCapacity on every append.
	acqSamples []acqSample
	// lastDisco is the default (by=artist) worst-first view; discoStale is the
	// block-level stale flag driven by that primary read.
	lastDisco   *goapi.DiscographyQuality
	discoStale  bool
	discoReason string
	updated     time.Time
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
		series:     discardSeries{},
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// acqSample is one observation of go-api's cumulative acquisition counters at a
// point in time. The windowed success rate reads the delta between two of these,
// so the lifetime totals never drive the headline on their own.
type acqSample struct {
	at        time.Time
	succeeded uint64
	failed    uint64
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Domain quality"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesDiscoSuccessRate
}

func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	var evalErr, acqErr error

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				evalErr = b.recoverSource(ctx, "eval", "GET /admin/eval", r, b.markEvalStale)
			}
		}()
		evalErr = b.collectEval(ctx)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				acqErr = b.recoverSource(ctx, "acquisition", "GET /admin/acquisition", r, b.markAcqStale)
			}
		}()
		acqErr = b.collectAcq(ctx)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				_ = b.recoverSource(ctx, "discography", "GET /admin/quality/discography", r, b.markDiscoStale)
			}
		}()
		b.collectDisco(ctx)
	}()
	wg.Wait()

	if evalErr != nil && acqErr != nil {
		return nil, fmt.Errorf("%w: eval=%s acquisition=%s", errBothDown, evalErr.Error(), acqErr.Error())
	}
	return nil, nil
}

func (b *Bucket) recoverSource(ctx context.Context, source, op string, r any, mark func(error) bool) error {
	err := fmt.Errorf("%s: recovered panic: %v", source, r)
	everMirrored := mark(err)
	b.logSourceUnreachable(ctx, source, op, everMirrored, err)
	return err
}

func (b *Bucket) collectEval(ctx context.Context) error {
	readCtx, cancel := context.WithTimeout(ctx, perReadTimeout(ctx))
	defer cancel()
	eval, err := b.reader.AdminEval(readCtx)
	if err != nil {
		everMirrored := b.markEvalStale(err)
		b.logSourceUnreachable(ctx, "eval", "GET /admin/eval", everMirrored, err)
		return err
	}
	b.recordEval(eval)
	return nil
}

func (b *Bucket) collectAcq(ctx context.Context) error {
	readCtx, cancel := context.WithTimeout(ctx, perReadTimeout(ctx))
	defer cancel()
	acq, err := b.reader.AdminAcquisition(readCtx)
	if err != nil {
		everMirrored := b.markAcqStale(err)
		b.logSourceUnreachable(ctx, "acquisition", "GET /admin/acquisition", everMirrored, err)
		return err
	}
	b.recordAcq(acq)
	return nil
}

func (b *Bucket) collectDisco(ctx context.Context) {
	readCtx, cancel := context.WithTimeout(ctx, perReadTimeout(ctx))
	defer cancel()
	disco, err := b.reader.AdminDiscographyQuality(readCtx)
	if err != nil {
		everMirrored := b.markDiscoStale(err)
		b.logSourceUnreachable(ctx, "discography", "GET /admin/quality/discography", everMirrored, err)
		return
	}
	b.recordDisco(disco)
	b.recordDiscoTrend(disco)
}

func perReadTimeout(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return perReadTimeoutFallback
	}
	remaining := time.Until(deadline)
	if remaining <= perReadTimeoutMargin {
		return remaining
	}
	return remaining - perReadTimeoutMargin
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
	Eval      *goapi.EvalStatus `json:"eval"`
	EvalStale bool              `json:"evalStale"`
	// EvalAgeStale flags a score go-api last computed longer ago than evalFreshness.
	// It is independent of EvalStale: a reachable read can still serve a stale score.
	EvalAgeStale bool                     `json:"evalAgeStale"`
	Acquisition  *goapi.AcquisitionStatus `json:"acquisition"`
	AcqStale     bool                     `json:"acqStale"`
	// AcqWindow is the recent-window success rate; nil when the window holds no
	// completed jobs, so the panel shows "no recent data" rather than a spurious 0%.
	AcqWindow   *AcqWindow                `json:"acqWindow"`
	Discography *goapi.DiscographyQuality `json:"discography"`
	DiscoStale  bool                      `json:"discoStale"`
	DiscoTrend  []core.Signal             `json:"discoTrend"`
}

// AcqWindow is the acquisition success rate over the recent window the panel
// renders as the acquisition headline instead of the lifetime ratio, so a spike of
// current failures is visible even while the all-time average stays high. Completed
// is the number of jobs that finished inside the window, shown as the rate's
// freshness.
type AcqWindow struct {
	Rate      float64 `json:"rate"`
	Completed uint64  `json:"completed"`
}

// Snapshot builds the domain-quality envelope. The two anchor reads (eval,
// acquisition) drive the state: both unreachable is source_down, either stale or
// not-yet-mirrored is stale, both fresh is live. UpdatedAt is the more recent of
// the two anchors' last successful read.
func (b *Bucket) Snapshot() core.Snapshot {
	now := b.now()
	b.mu.RLock()
	eval, evalStale, evalReason := b.lastEval, b.evalStale, b.evalReason
	acq, acqStale, acqReason := b.lastAcq, b.acqStale, b.acqReason
	disco, discoStale, discoReason := b.lastDisco, b.discoStale, b.discoReason
	updated := b.updated
	acqWindow := b.acqWindowRate(now)
	b.mu.RUnlock()

	evalAgeStale := eval != nil && eval.StaleByAge(now, evalFreshness)
	severity, headline := domainHealth(eval, acqWindow, disco)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     domainState(eval, evalStale, acq, acqStale),
		Reason:    firstNonEmptyReason(evalReason, acqReason, discoReason),
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data: core.MarshalData(Data{
			Eval:         eval,
			EvalStale:    evalStale,
			EvalAgeStale: evalAgeStale,
			Acquisition:  acq,
			AcqStale:     acqStale,
			AcqWindow:    acqWindow,
			Discography:  disco,
			DiscoStale:   discoStale,
			DiscoTrend:   b.discoTrend.Snapshot(),
		}),
	}
}

// acqWindowRate is the acquisition success rate over acqWindow: the delta between
// the earliest sample still inside the window and the latest. It returns nil when
// the window holds fewer than two samples, spans a counter reset, or saw no
// completed jobs — so the panel shows "no recent data" rather than a spurious 0%.
// Callers hold at least b.mu's read lock.
func (b *Bucket) acqWindowRate(now time.Time) *AcqWindow {
	base, ok := b.earliestAcqSince(now.Add(-acqWindow))
	if !ok {
		return nil
	}
	latest := b.acqSamples[len(b.acqSamples)-1]
	curr := goapi.AcquisitionStatus{Succeeded: latest.succeeded, Failed: latest.failed}
	prev := goapi.AcquisitionStatus{Succeeded: base.succeeded, Failed: base.failed}
	rate, defined := curr.SuccessRateSince(prev)
	if !defined {
		return nil
	}
	completed := (latest.succeeded - base.succeeded) + (latest.failed - base.failed)
	return &AcqWindow{Rate: rate, Completed: completed}
}

// earliestAcqSince returns the oldest retained acquisition sample at or after
// cutoff — the window's baseline — reporting false when none is inside the window.
func (b *Bucket) earliestAcqSince(cutoff time.Time) (acqSample, bool) {
	for _, s := range b.acqSamples {
		if !s.at.Before(cutoff) {
			return s, true
		}
	}
	return acqSample{}, false
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

func firstNonEmptyReason(reasons ...string) string {
	for _, r := range reasons {
		if r != "" {
			return r
		}
	}
	return ""
}

// domainHealth grades the product itself, not the freshness of the reads. Each
// side is graded on its own and the worst one wins, so the headline is always the
// number that drove the verdict rather than an unrelated healthy figure. A side
// with nothing rateable (never mirrored, or no completed jobs to divide by) is
// skipped rather than counted as healthy.
func domainHealth(eval *goapi.EvalStatus, acqWindow *AcqWindow, disco *goapi.DiscographyQuality) (core.Severity, string) {
	worst := worstGrade(suspectGrade(disco), acquisitionGrade(acqWindow), evalGrade(eval))
	return worst.severity, worst.headline
}

// grade is one measured aspect of domain quality: its severity and the figure
// that justifies it. An empty headline marks a measure that could not be taken.
type grade struct {
	severity core.Severity
	headline string
}

// worstGrade picks the most severe measure that could be taken, ties going to the
// earlier argument. With nothing rateable it says so rather than claiming health.
func worstGrade(grades ...grade) grade {
	worst := grade{severity: core.SeverityOK, headline: "no quality signal yet"}
	taken := false
	for _, g := range grades {
		if g.headline == "" {
			continue
		}
		if !taken || g.severity.Worse(worst.severity) {
			worst, taken = g, true
		}
	}
	return worst
}

// suspectGrade grades go-api's windowed suspect rate — the share of real
// discography opens whose top release-suspect fired, which the panel already calls
// this bucket's headline. The thresholds are the panel's own traffic-light bands
// (web/src/panels/domainquality.panel.tsx severityColor), kept here so the grade
// and the colour cannot drift apart.
func suspectGrade(d *goapi.DiscographyQuality) grade {
	if d == nil {
		return grade{}
	}
	headline := fmt.Sprintf("suspect rate %.0f%%", d.SuspectRate*100)
	switch {
	case d.SuspectRate >= criticalSuspectRate:
		return grade{core.SeverityCritical, headline}
	case d.SuspectRate >= warnSuspectRate:
		return grade{core.SeverityWarn, headline}
	default:
		return grade{core.SeverityOK, headline}
	}
}

// acquisitionGrade grades the recent-window acquisition success rate against the
// panel's own bands (severityColorForRate). A nil window — no recent completions,
// or fewer than two samples on startup — is skipped rather than read as a 0%
// failure, so an idle acquisition loop never pages.
func acquisitionGrade(w *AcqWindow) grade {
	if w == nil {
		return grade{}
	}
	headline := fmt.Sprintf("acquisition success %.0f%%", w.Rate*100)
	switch {
	case w.Rate < criticalAcqRate:
		return grade{core.SeverityCritical, headline}
	case w.Rate < warnAcqRate:
		return grade{core.SeverityWarn, headline}
	default:
		return grade{core.SeverityOK, headline}
	}
}

// evalGrade grades search quality against go-api's own baseline: a score below it
// is a product regression, which warns rather than pages — nothing is down, the
// results just got worse. A meter with no score or no baseline has nothing to
// compare, so the measure is skipped.
func evalGrade(e *goapi.EvalStatus) grade {
	if e == nil || e.Score == nil || e.Baseline == nil {
		return grade{}
	}
	score, baseline := *e.Score, *e.Baseline
	headline := fmt.Sprintf("eval %.2f vs baseline %.2f", score, baseline)
	if score < baseline {
		return grade{core.SeverityWarn, headline}
	}
	return grade{core.SeverityOK, headline}
}

// recordEval stores the latest eval status and clears its stale flag. The
// snapshot is copied to the heap and only ever replaced (never mutated in place),
// so Render may read the pointer under the lock and use it after.
func (b *Bucket) recordEval(e goapi.EvalStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastEval = &e
	b.evalStale = false
	b.evalReason = ""
	now := b.now()
	b.updated = now
	if e.Score != nil {
		b.series.Record(bucketID, seriesEvalScore, core.Point{At: now, Value: *e.Score})
	}
}

// recordAcq stores the latest acquisition snapshot, clears its stale flag, and
// folds the cumulative counters into the bounded sample ring the windowed rate
// reads. Same replace-never-mutate discipline as recordEval.
func (b *Bucket) recordAcq(a goapi.AcquisitionStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastAcq = &a
	b.acqStale = false
	b.acqReason = ""
	now := b.now()
	b.updated = now
	b.appendAcqSample(a)
	if w := b.acqWindowRate(now); w != nil {
		b.series.Record(bucketID, seriesAcquisitionRate, core.Point{At: now, Value: w.Rate})
	}
}

// appendAcqSample records the latest cumulative counters and trims the ring to
// acqSampleCapacity, so the samples that back the windowed rate are bounded by
// construction however long the service runs. Callers hold b.mu.
func (b *Bucket) appendAcqSample(a goapi.AcquisitionStatus) {
	b.acqSamples = append(b.acqSamples, acqSample{at: b.now(), succeeded: a.Succeeded, failed: a.Failed})
	if len(b.acqSamples) > acqSampleCapacity {
		b.acqSamples = b.acqSamples[len(b.acqSamples)-acqSampleCapacity:]
	}
}

// recordDisco stores the latest discography-quality snapshot and clears its stale
// flag, with the same replace-never-mutate discipline as recordEval.
func (b *Bucket) recordDisco(d goapi.DiscographyQuality) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastDisco = &d
	b.discoStale = false
	b.discoReason = ""
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
	b.series.Record(bucketID, seriesDiscoSuccessRate, core.Point{At: b.now(), Value: 1 - d.SuspectRate})
}

// markDiscoStale flags the discography side stale while preserving its last-known
// value, reporting whether the side has ever mirrored a value.
func (b *Bucket) markDiscoStale(err error) (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.discoStale = true
	b.discoReason = goapi.Classify(err)
	return b.lastDisco != nil
}

// markEvalStale flags the eval side stale while preserving its last-known value —
// degrade-don't-crash: serve last-known flagged stale rather than dropping it. It
// reports whether the side has ever mirrored a value, so a source that has never
// once succeeded can be surfaced distinctly rather than failing invisibly.
func (b *Bucket) markEvalStale(err error) (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evalStale = true
	b.evalReason = goapi.Classify(err)
	return b.lastEval != nil
}

// markAcqStale flags the acquisition side stale while preserving its last-known
// value, reporting whether the side has ever mirrored a value.
func (b *Bucket) markAcqStale(err error) (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acqStale = true
	b.acqReason = goapi.Classify(err)
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

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
