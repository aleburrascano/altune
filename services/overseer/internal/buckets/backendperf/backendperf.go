// Package backendperf is the Overseer's Back-end performance bucket: at a glance,
// how fast each go-api route is (p50/p95/p99) and how much traffic it carries, so
// a slow path is obvious before users feel it. It reads go-api's operator-only
// GET /admin/metrics/live (the per-route latency histogram built by the
// metrics-enabler epic) via the read-only goapi client, estimates per-route
// percentiles from the fixed histogram buckets, highlights the slowest routes,
// and keeps a bounded throughput trend. When the metrics read is unreachable it
// serves the last-known latency flagged STALE rather than going dark.
//
// Percentiles are estimated from the endpoint's bounded histogram, never from raw
// samples (the endpoint exposes no raw samples by design). The bucket owns all its
// own files and self-registers with one blank import in the composition root (the
// additive-buckets invariant).
package backendperf

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

// historyCapacity bounds the retained throughput samples. The ring caps memory by
// construction no matter how long the service runs.
const historyCapacity = 120

// errUnconfigured is the transport error the null reader reports when go-api is
// not configured: the bucket renders stale rather than failing the whole service
// at startup.
var errUnconfigured = errors.New("backendperf: go-api not configured")

// metricsReader is the seam onto the live-metrics read: the single goapi method
// the bucket needs. Depending on the interface (not the concrete *goapi.Client)
// lets a test drive it deterministically with no network.
type metricsReader interface {
	AdminMetricsLive(ctx context.Context) (goapi.LiveMetrics, error)
}

// Bucket reads go-api's per-route latency histogram, estimates p50/p95/p99 per
// route, and renders them with the slowest routes highlighted alongside a bounded
// throughput trend.
type Bucket struct {
	reader  metricsReader
	history core.Store

	// mu guards the last-known latency snapshot and its stale flag, which the
	// collect loop writes and the HTTP render reads.
	mu    sync.RWMutex
	last  goapi.LatencyMetrics
	have  bool
	stale bool
}

// New builds the Back-end performance bucket from the environment. When go-api is
// not configured it falls back to a null reader: the bucket still registers and
// renders stale rather than crashing.
func New() *Bucket { return newBucket(readerFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// reader; production goes through New.
func newBucket(reader metricsReader) *Bucket {
	return &Bucket{
		reader:  reader,
		history: core.NewRingStore(historyCapacity),
	}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "backendperf", Title: "Back-end performance"}
}

// Collect reads the live per-route latency histogram. On success it records the
// fresh snapshot (rendered for percentiles) and returns a bounded throughput
// signal; when the read is unreachable it flags the view stale and returns an
// error, so the shell keeps the last-known panel and Render marks it STALE.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	live, err := b.reader.AdminMetricsLive(ctx)
	if err != nil {
		b.markStale()
		return nil, fmt.Errorf("backendperf: live metrics unreachable: %w", err)
	}
	b.recordFresh(live.Latency)
	return []core.Signal{throughputSignal(live.Latency)}, nil
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.history.Add(s)
	}
}

// Render builds the panel from the last-known latency snapshot (percentiles +
// slowest-route highlight, flagged stale when the read is currently unreachable)
// and the bounded throughput trend.
func (b *Bucket) Render() core.Panel {
	b.mu.RLock()
	last := b.last
	have := b.have
	stale := b.stale
	b.mu.RUnlock()

	var stats []routeStat
	if have {
		stats = routeStats(last)
	}
	return core.Panel{Title: b.Meta().Title, Body: renderBody(stats, b.history.Snapshot(), stale)}
}

// recordFresh stores the latest latency snapshot and clears the stale flag. The
// snapshot is only ever replaced (never mutated in place), so Render may copy the
// struct under the lock and read its map after releasing it.
func (b *Bucket) recordFresh(m goapi.LatencyMetrics) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last = m
	b.have = true
	b.stale = false
}

// markStale flags the view stale while preserving the last-known snapshot — the
// degrade-don't-crash behaviour: serve last-known flagged stale rather than
// dropping the panel.
func (b *Bucket) markStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale = true
}

// throughputSignal captures total observed requests across all routes at collect
// time, for the bounded throughput trend. The counts are cumulative since go-api
// start, so the trend reflects traffic accrued between reads.
func throughputSignal(m goapi.LatencyMetrics) core.Signal {
	var total uint64
	for _, rl := range m.Routes {
		total += rl.Count
	}
	return core.Signal{
		At:   time.Now().UTC(),
		Kind: "throughput",
		Text: fmt.Sprintf("%d requests across %d route(s)", total, len(m.Routes)),
	}
}

// readerFromEnv builds the read-only goapi client from OVERSEER_GOAPI_URL and the
// process-wide operator token source. Missing or invalid config yields a null
// reader so an unconfigured bucket degrades to source-down instead of failing the
// whole service at startup. Config normally lives in the config package, which
// this leaf may not edit; reading the go-api URL here and taking the credential
// from goapi.SharedTokenSource keeps the change within the bucket, matching the
// other buckets.
func readerFromEnv() metricsReader {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullReader{}
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		// Degrade to source-down, but say why: without this a URL typo is
		// indistinguishable from go-api being genuinely down.
		slog.Warn("backendperf: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return nullReader{}
	}
	return c
}

// nullReader stands in when go-api is unconfigured: every read reports
// source-down, so an unconfigured bucket renders stale rather than nil-panicking.
type nullReader struct{}

func (nullReader) AdminMetricsLive(context.Context) (goapi.LiveMetrics, error) {
	return goapi.LiveMetrics{}, &goapi.SourceDownError{Op: "GET /admin/metrics/live", Err: errUnconfigured}
}

func init() { core.Register(New()) }
