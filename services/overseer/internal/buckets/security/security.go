package security

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"log/slog"
	"os"
	"strings"
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

func New() *Bucket { return newBucket(clientFromEnv(), defaultSuite(), intervalFromEnv()) }

func newBucket(client prober, checks []check, interval time.Duration) *Bucket {
	b := &Bucket{history: core.NewRingStore(historyCapacity), series: discardSeries{}}
	b.scheduler = newScheduler(client, checks, interval, b.record)
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

func clientFromEnv() prober {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullProber{}
	}
	c, err := newFencedClient(base, allowlistFromEnv(base))
	if err != nil {
		slog.Warn("security: cannot build fenced client, degrading to source-down", "error", err)
		return nullProber{}
	}
	return c
}

func allowlistFromEnv(base string) []string {
	raw := strings.TrimSpace(os.Getenv("OVERSEER_SECURITY_ALLOWLIST"))
	if raw == "" {
		return []string{base}
	}
	return strings.Split(raw, ",")
}

func intervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OVERSEER_SECURITY_INTERVAL"))
	if raw == "" {
		return defaultInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("security: invalid OVERSEER_SECURITY_INTERVAL, using default",
			"value", raw, "default", defaultInterval)
		return defaultInterval
	}
	return d
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
