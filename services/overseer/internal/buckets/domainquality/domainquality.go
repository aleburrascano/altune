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

const discoTrendCapacity = 120

const (
	bucketID               = "domainquality"
	seriesDiscoSuccessRate = "disco_success_rate"
	seriesEvalScore        = "eval_score"
	seriesAcquisitionRate  = "acquisition_rate"
)

const (
	warnSuspectRate     = 0.2
	criticalSuspectRate = 0.5
	warnAcqRate         = 0.9
	criticalAcqRate     = 0.7
)

const evalFreshness = 12 * time.Hour

const (
	perReadTimeoutFallback = 7 * time.Second
	perReadTimeoutMargin   = 500 * time.Millisecond
)

const (
	acqWindow         = 10 * time.Minute
	acqSampleCapacity = 300
)

var errUnconfigured = errors.New("domainquality: go-api not configured")

var errBothDown = errors.New("domainquality: eval and acquisition both unreachable")

type reader interface {
	AdminEval(ctx context.Context) (goapi.EvalStatus, error)
	AdminAcquisition(ctx context.Context) (goapi.AcquisitionStatus, error)
	AdminDiscographyQuality(ctx context.Context) (goapi.DiscographyQuality, error)
}

type Bucket struct {
	reader     reader
	discoTrend core.Store
	series     core.Series

	now func() time.Time

	mu          sync.RWMutex
	lastEval    *goapi.EvalStatus
	evalStale   bool
	evalReason  string
	lastAcq     *goapi.AcquisitionStatus
	acqStale    bool
	acqReason   string
	acqSamples  []acqSample
	lastDisco   *goapi.DiscographyQuality
	discoStale  bool
	discoReason string
	updated     time.Time
}

func New() *Bucket { return newBucket(readerFromEnv()) }

func newBucket(r reader) *Bucket {
	return &Bucket{
		reader:     r,
		discoTrend: core.NewRingStore(discoTrendCapacity),
		series:     discardSeries{},
		now:        func() time.Time { return time.Now().UTC() },
	}
}

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
				evalErr = b.recoverSource(ctx, "eval", "GET /observe/eval", r, b.markEvalStale)
			}
		}()
		evalErr = b.collectEval(ctx)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				acqErr = b.recoverSource(ctx, "acquisition", "GET /observe/acquisition", r, b.markAcqStale)
			}
		}()
		acqErr = b.collectAcq(ctx)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				_ = b.recoverSource(ctx, "discography", "GET /observe/quality/discography", r, b.markDiscoStale)
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
		b.logSourceUnreachable(ctx, "eval", "GET /observe/eval", everMirrored, err)
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
		b.logSourceUnreachable(ctx, "acquisition", "GET /observe/acquisition", everMirrored, err)
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
		b.logSourceUnreachable(ctx, "discography", "GET /observe/quality/discography", everMirrored, err)
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

func (b *Bucket) Store([]core.Signal) {}

type Data struct {
	Eval         *goapi.EvalStatus         `json:"eval"`
	EvalStale    bool                      `json:"evalStale"`
	EvalAgeStale bool                      `json:"evalAgeStale"`
	Acquisition  *goapi.AcquisitionStatus  `json:"acquisition"`
	AcqStale     bool                      `json:"acqStale"`
	AcqWindow    *AcqWindow                `json:"acqWindow"`
	Discography  *goapi.DiscographyQuality `json:"discography"`
	DiscoStale   bool                      `json:"discoStale"`
	DiscoTrend   []core.Signal             `json:"discoTrend"`
}

type AcqWindow struct {
	Rate      float64 `json:"rate"`
	Completed uint64  `json:"completed"`
}

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

func (b *Bucket) earliestAcqSince(cutoff time.Time) (acqSample, bool) {
	for _, s := range b.acqSamples {
		if !s.at.Before(cutoff) {
			return s, true
		}
	}
	return acqSample{}, false
}

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

func domainHealth(eval *goapi.EvalStatus, acqWindow *AcqWindow, disco *goapi.DiscographyQuality) (core.Severity, string) {
	worst := worstGrade(suspectGrade(disco), acquisitionGrade(acqWindow), evalGrade(eval))
	return worst.severity, worst.headline
}

type grade struct {
	severity core.Severity
	headline string
}

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

func (b *Bucket) recordEval(e goapi.EvalStatus) {
	b.mu.Lock()
	b.lastEval = &e
	b.evalStale = false
	b.evalReason = ""
	now := b.now()
	b.updated = now
	score := e.Score
	b.mu.Unlock()
	if score != nil {
		b.series.Record(bucketID, seriesEvalScore, core.Point{At: now, Value: *score})
	}
}

func (b *Bucket) recordAcq(a goapi.AcquisitionStatus) {
	b.mu.Lock()
	b.lastAcq = &a
	b.acqStale = false
	b.acqReason = ""
	now := b.now()
	b.updated = now
	b.appendAcqSample(a)
	w := b.acqWindowRate(now)
	b.mu.Unlock()
	if w != nil {
		b.series.Record(bucketID, seriesAcquisitionRate, core.Point{At: now, Value: w.Rate})
	}
}

func (b *Bucket) appendAcqSample(a goapi.AcquisitionStatus) {
	b.acqSamples = append(b.acqSamples, acqSample{at: b.now(), succeeded: a.Succeeded, failed: a.Failed})
	if len(b.acqSamples) > acqSampleCapacity {
		b.acqSamples = b.acqSamples[len(b.acqSamples)-acqSampleCapacity:]
	}
}

func (b *Bucket) recordDisco(d goapi.DiscographyQuality) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastDisco = &d
	b.discoStale = false
	b.discoReason = ""
}

func (b *Bucket) recordDiscoTrend(d goapi.DiscographyQuality) {
	if sig, ok := discoTrendSignal(d); ok {
		b.discoTrend.Add(sig)
	}
	b.series.Record(bucketID, seriesDiscoSuccessRate, core.Point{At: b.now(), Value: 1 - d.SuspectRate})
}

func (b *Bucket) markDiscoStale(err error) (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.discoStale = true
	b.discoReason = goapi.Classify(err)
	return b.lastDisco != nil
}

func (b *Bucket) markEvalStale(err error) (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evalStale = true
	b.evalReason = goapi.Classify(err)
	return b.lastEval != nil
}

func (b *Bucket) markAcqStale(err error) (everMirrored bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acqStale = true
	b.acqReason = goapi.Classify(err)
	return b.lastAcq != nil
}

func (b *Bucket) logSourceUnreachable(ctx context.Context, source, op string, everMirrored bool, err error) {
	slog.WarnContext(ctx, "domainquality.source.unreachable",
		"source", source,
		"op", op,
		"never_mirrored", !everMirrored,
		"error", err.Error(),
	)
}

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

func readerFromEnv() reader {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullReader{}
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("domainquality: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return nullReader{}
	}
	return c
}

type nullReader struct{}

func (nullReader) AdminEval(context.Context) (goapi.EvalStatus, error) {
	return goapi.EvalStatus{}, &goapi.SourceDownError{Op: "GET /observe/eval", Err: errUnconfigured}
}

func (nullReader) AdminAcquisition(context.Context) (goapi.AcquisitionStatus, error) {
	return goapi.AcquisitionStatus{}, &goapi.SourceDownError{Op: "GET /observe/acquisition", Err: errUnconfigured}
}

func (nullReader) AdminDiscographyQuality(context.Context) (goapi.DiscographyQuality, error) {
	return goapi.DiscographyQuality{}, &goapi.SourceDownError{Op: "GET /observe/quality/discography", Err: errUnconfigured}
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
