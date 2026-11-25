package security

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	historyCapacity = 120
	defaultInterval = time.Hour

	bucketID            = "security"
	seriesFindingsOpen  = "findings_open"
	seriesProbeFailures = "probe_failures"
)

type Bucket struct {
	scheduler *scheduler
	history   *core.RingStore
	series    core.Series

	mu     sync.RWMutex
	last   *suiteResult
	stale  bool
	reason string

	start sync.Once
}

type prober interface {
	Probe(ctx context.Context, path, rawQuery string) (int, error)
}

type Deps struct {
	Prober   prober
	Interval time.Duration
	Now      func() time.Time
}

func New(deps Deps) *Bucket {
	probe := deps.Prober
	if probe == nil {
		probe = nullProber{}
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	b := &Bucket{history: core.NewRingStore(historyCapacity), series: core.DiscardSeries{}}
	b.scheduler = newScheduler(probe, defaultSuite(), deps.Interval, now, b.record)
	return b
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Security"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesFindingsOpen
}

func (b *Bucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{"history": b.history}
}

func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.scheduler.run(ctx) })
}

func (b *Bucket) Collect(context.Context) ([]core.Signal, error) {
	return nil, nil
}

func (b *Bucket) Store([]core.Signal) {}

func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	last, stale, reason := b.last, b.stale, b.reason
	b.mu.RUnlock()

	data := suiteData(last)
	data.History = b.history.Snapshot()
	updated := time.Time{}
	if last != nil {
		updated = last.at
	}
	severity, headline := securityHealth(last)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.StaleState(stale, last != nil),
		Reason:    reason,
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data:      core.MarshalData(data),
	}
}

func (b *Bucket) record(res suiteResult) {
	b.recordSeries(res)
	if !res.reachedAny() {
		b.markStale()
		return
	}
	b.mu.Lock()
	r := res
	b.last, b.stale, b.reason = &r, false, ""
	b.mu.Unlock()
	b.history.Add(summarySignal(res))
}

func (b *Bucket) recordSeries(res suiteResult) {
	b.series.Record(bucketID, seriesFindingsOpen, core.Point{At: res.at, Value: float64(res.failing())})
	b.series.Record(bucketID, seriesProbeFailures, core.Point{At: res.at, Value: float64(res.unreached())})
}

func (b *Bucket) markStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale = true
	b.reason = goapi.ReasonDown
}

var errUnconfigured = errors.New("security: go-api not configured")

type nullProber struct{}

func (nullProber) Probe(context.Context, string, string) (int, error) {
	return 0, fmt.Errorf("security: go-api unreachable: %w", errUnconfigured)
}
