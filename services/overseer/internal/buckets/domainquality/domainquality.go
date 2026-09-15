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
}

// Bucket mirrors go-api's eval-meter score and acquisition success rate into a
// bounded ring and renders them, each side independently flagged stale when its
// read is currently unreachable.
type Bucket struct {
	reader  reader
	history core.Store

	// mu guards the last-known eval/acquisition snapshots and their stale flags,
	// which the collect loop writes and the HTTP render reads.
	mu        sync.RWMutex
	lastEval  *goapi.EvalStatus
	evalStale bool
	lastAcq   *goapi.AcquisitionStatus
	acqStale  bool
}

// New builds the Domain-quality bucket from the environment. When go-api is not
// configured it falls back to a null client: the bucket still registers and both
// reads render stale rather than crashing.
func New() *Bucket { return newBucket(readerFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// reader; production goes through New.
func newBucket(r reader) *Bucket {
	return &Bucket{reader: r, history: core.NewRingStore(historyCapacity)}
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
		b.markEvalStale()
	} else {
		b.recordEval(eval)
		signals = append(signals, evalSignal(eval))
	}

	acq, acqErr := b.reader.AdminAcquisition(ctx)
	if acqErr != nil {
		b.markAcqStale()
	} else {
		b.recordAcq(acq)
		signals = append(signals, acqSignal(acq))
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
	b.mu.RUnlock()

	history := b.history.Snapshot()
	return core.Panel{
		Title: b.Meta().Title,
		Body:  renderBody(eval, evalStale, acq, acqStale, history),
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

// markEvalStale flags the eval side stale while preserving its last-known value —
// degrade-don't-crash: serve last-known flagged stale rather than dropping it.
func (b *Bucket) markEvalStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evalStale = true
}

// markAcqStale flags the acquisition side stale while preserving its last-known
// value.
func (b *Bucket) markAcqStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acqStale = true
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

// readerFromEnv builds the read-only goapi client from OVERSEER_GOAPI_URL and
// OVERSEER_GOAPI_TOKEN. Missing or invalid config yields a null client so an
// unconfigured bucket degrades to source-down instead of failing the whole
// service at startup. Config normally lives in the config package, which this
// leaf may not edit; reading the two go-api knobs here keeps the change within
// the bucket, matching the Reliability and Usage buckets.
func readerFromEnv() reader {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	token := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_TOKEN"))
	if base == "" || token == "" {
		return nullReader{}
	}
	c, err := goapi.New(base, goapi.StaticTokenSource(token))
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

func init() { core.Register(New()) }
