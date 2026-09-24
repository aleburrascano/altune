// Package backendperf is the Overseer's Back-end performance bucket: at a glance,
// how fast each go-api route is (p50/p95/p99) and how much traffic it carries, so
// a slow path is obvious before users feel it. It reads go-api's operator-only
// GET /admin/metrics/live (the per-route latency histogram built by the
// metrics-enabler epic) via the read-only goapi client, estimates per-route
// percentiles from the fixed histogram buckets, highlights the slowest routes,
// and keeps a bounded throughput trend. When the metrics read is unreachable it
// serves the last-known latency flagged STALE rather than going dark.
//
// Latency and traffic are windowed to the recent interval, not lifetime totals:
// go-api's histogram is cumulative since start, so the bucket keeps the previous
// read and renders the delta current-minus-previous. That way a spike today is not
// diluted by weeks of good samples, and traffic reads as a rate rather than an
// ever-growing count.
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

// The latency bands (ms) the bucket grades its slowest route's p99 on, hoisted
// from the panel's own traffic lights (web/src/panels/backendperf.panel.tsx) so
// one change moves the grade and the colour together.
const (
	warnP99Ms     = 100.0
	criticalP99Ms = 500.0
)

// The 5xx error-rate bands the bucket grades its worst route on, hoisted from the
// panel's own traffic lights (web/src/panels/backendperf.panel.tsx) alongside the
// latency bands so the grade and the colour move together. A fast route serving
// errors is a failure the latency bands alone cannot see.
const (
	warnErrorRate     = 0.01
	criticalErrorRate = 0.05
)

// minErrorSamples is the fewest classified responses a route must serve within one
// window before its 5xx rate may grade the bucket at all. A window is one tick
// (5s by default), so a near-idle route can carry a single request: one 5xx then
// reads 100% and would page on nothing. The rate is still computed and shown below
// the floor — the panel marks it provisional (web/src/panels/backendperf.panel.tsx
// mirrors this constant) — it just does not raise severity.
const minErrorSamples = 10

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
	mu      sync.RWMutex
	last    goapi.LatencyMetrics
	have    bool
	stale   bool
	reason  string
	updated time.Time

	// prev is the previous cumulative read the window subtracts against, and
	// prevAt is when it was taken (kept with its monotonic reading, so a
	// wall-clock jump cannot distort the request rate). These are touched only by
	// the single collect goroutine — Snapshot never reads them — so they need no
	// lock.
	prev     goapi.LatencyMetrics
	prevAt   time.Time
	havePrev bool
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

// Collect reads the live per-route latency histogram. On success it windows the
// cumulative read against the previous one, records the recent-window snapshot
// (rendered for percentiles) and returns a bounded requests-per-second signal;
// when the read is unreachable it flags the view stale and returns an error, so
// the shell keeps the last-known panel and Render marks it STALE. The window is
// left un-advanced on failure, so the next good read spans across the gap.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	live, err := b.reader.AdminMetricsLive(ctx)
	if err != nil {
		b.markStale(err)
		return nil, fmt.Errorf("backendperf: live metrics unreachable: %w", err)
	}
	w := b.advanceWindow(live.Latency, time.Now())
	b.recordFresh(w.latency)
	return []core.Signal{throughputSignal(w)}, nil
}

// window is one collect's recent-window traffic: the per-route histogram delta
// since the previous read, the request count that delta carries, and the rate it
// implies over the interval.
type window struct {
	latency   goapi.LatencyMetrics
	requests  uint64
	perSecond float64
	at        time.Time
}

// advanceWindow computes the recent-window traffic — the per-route histogram delta
// since the previous read and the request rate over that interval — then rolls the
// stored cumulative snapshot forward. The first read has no predecessor, so the
// whole cumulative snapshot is the initial window and its rate stays zero (there is
// no interval to divide by yet). Elapsed time uses the monotonic reading carried by
// at, so a wall-clock jump cannot distort the rate.
func (b *Bucket) advanceWindow(cur goapi.LatencyMetrics, at time.Time) window {
	delta := windowedLatency(b.prev, cur, b.havePrev)
	requests := totalRequests(delta)
	var perSecond float64
	if b.havePrev {
		if secs := at.Sub(b.prevAt).Seconds(); secs > 0 {
			perSecond = float64(requests) / secs
		}
	}
	b.prev, b.prevAt, b.havePrev = cur, at, true
	return window{latency: delta, requests: requests, perSecond: perSecond, at: at.UTC()}
}

// windowedLatency returns the per-route histogram delta between the previous and
// current cumulative reads — the traffic in the recent window. The first read has
// no predecessor, so the whole cumulative snapshot is the initial window. A route
// missing from the previous read is carried whole (it is new), and a route whose
// cumulative count fell — go-api restarted and reset its counters — is treated as
// fresh, so a reset can never underflow the unsigned delta into a spurious spike.
func windowedLatency(prev, cur goapi.LatencyMetrics, havePrev bool) goapi.LatencyMetrics {
	if !havePrev {
		return cur
	}
	routes := make(map[string]goapi.RouteLatency, len(cur.Routes))
	for route, c := range cur.Routes {
		p, ok := prev.Routes[route]
		if !ok || c.Count < p.Count {
			routes[route] = c
			continue
		}
		routes[route] = deltaRoute(p, c)
	}
	return goapi.LatencyMetrics{Routes: routes}
}

// deltaRoute subtracts one route's previous cumulative counters from its current
// ones, matching histogram buckets by their le_ms label so a bucket added or
// reordered between reads cannot misalign the arithmetic.
func deltaRoute(prev, cur goapi.RouteLatency) goapi.RouteLatency {
	prevBucket := make(map[string]uint64, len(prev.Buckets))
	for _, b := range prev.Buckets {
		prevBucket[b.LeMs] = b.Count
	}
	buckets := make([]goapi.LatencyBucket, len(cur.Buckets))
	for i, b := range cur.Buckets {
		buckets[i] = goapi.LatencyBucket{LeMs: b.LeMs, Count: monotonicDelta(prevBucket[b.LeMs], b.Count)}
	}
	return goapi.RouteLatency{
		Count:   monotonicDelta(prev.Count, cur.Count),
		SumMs:   monotonicDelta(prev.SumMs, cur.SumMs),
		Buckets: buckets,
		Status:  deltaStatus(prev.Status, cur.Status),
	}
}

// deltaStatus subtracts one route's previous 2xx/4xx/5xx tally from its current
// one, per class, so the error rate is computed over the recent window rather than
// the lifetime totals. Each class deltas independently through monotonicDelta, so a
// counter reset on any one class cannot underflow into a spurious spike.
func deltaStatus(prev, cur goapi.StatusClasses) goapi.StatusClasses {
	return goapi.StatusClasses{
		Count2xx: monotonicDelta(prev.Count2xx, cur.Count2xx),
		Count4xx: monotonicDelta(prev.Count4xx, cur.Count4xx),
		Count5xx: monotonicDelta(prev.Count5xx, cur.Count5xx),
	}
}

// monotonicDelta returns cur-prev for two readings of a monotonic counter, falling
// back to cur when the counter went backwards (a reset), so the unsigned subtraction
// can never wrap into a huge bogus count.
func monotonicDelta(prev, cur uint64) uint64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}

// totalRequests sums the per-route request counts: the traffic the window carries.
func totalRequests(m goapi.LatencyMetrics) uint64 {
	var total uint64
	for _, rl := range m.Routes {
		total += rl.Count
	}
	return total
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.history.Add(s)
	}
}

// Data is the back-end performance payload: per-route recent-window latency stats
// sorted slowest-first, and the bounded requests-per-second trend. Route templates
// are watched-app data carried raw; React escapes them.
type Data struct {
	Routes     []routeStat   `json:"routes"`
	Throughput []core.Signal `json:"throughput"`
}

// Snapshot builds the back-end performance envelope from the last-known latency
// snapshot and the bounded throughput trend. When the live-metrics read is
// currently unreachable the panel is source_down (last known latency preserved);
// with no snapshot yet it is stale; otherwise live.
func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	last := b.last
	have := b.have
	stale := b.stale
	reason := b.reason
	updated := b.updated
	b.mu.RUnlock()

	var stats []routeStat
	if have {
		stats = routeStats(last)
	}
	severity, headline := backendperfHealth(stats)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.StaleState(stale, have),
		Reason:    reason,
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data:      core.MarshalData(Data{Routes: stats, Throughput: b.history.Snapshot()}),
	}
}

// backendperfHealth grades the bucket on two independent axes — the slowest
// route's p99 and the worst sufficiently-sampled route's 5xx error rate — and
// leads with whichever is more severe, so a fast route serving errors is never
// masked by healthy latency. Routes below the sample floor sit out the error axis
// entirely; with no route above it, latency alone grades. Both axes use the same
// bands the panel colours by (web/src/panels/backendperf.panel.tsx), so the grade
// and the colour cannot drift.
func backendperfHealth(stats []routeStat) (core.Severity, string) {
	if len(stats) == 0 {
		return core.SeverityOK, "no route latency yet"
	}
	latSev, latLine := latencyHealth(stats[0])
	worst, gradable := worstGradableErrorRate(stats)
	if !gradable {
		return latSev, latLine
	}
	errSev, errLine := errorRateHealth(worst)
	if errSev.Worse(latSev) {
		return errSev, errLine
	}
	return latSev, latLine
}

// latencyHealth grades one route's p99 against the latency bands. Stats arrive
// sorted slowest-first, so the caller passes the head — the worst route by p99.
func latencyHealth(worst routeStat) (core.Severity, string) {
	headline := fmt.Sprintf("slowest p99 %s — %s", formatMs(worst.P99), worst.Route)
	switch {
	case worst.P99.Ms >= criticalP99Ms:
		return core.SeverityCritical, headline
	case worst.P99.Ms >= warnP99Ms:
		return core.SeverityWarn, headline
	default:
		return core.SeverityOK, headline
	}
}

// errorRateHealth grades one route's 5xx error rate against the error-rate bands.
func errorRateHealth(worst routeStat) (core.Severity, string) {
	headline := fmt.Sprintf("error rate %s — %s", formatRate(worst.ErrorRate), worst.Route)
	switch {
	case worst.ErrorRate >= criticalErrorRate:
		return core.SeverityCritical, headline
	case worst.ErrorRate >= warnErrorRate:
		return core.SeverityWarn, headline
	default:
		return core.SeverityOK, headline
	}
}

// worstGradableErrorRate returns the route with the highest 5xx error rate among
// those that served at least minErrorSamples classified responses in the window,
// since the slowest route is not necessarily the one failing most. Routes below
// the floor are skipped rather than merely capped, so a near-idle route reading
// 100% cannot hide a busy route genuinely failing beneath it. The bool is false
// when no route cleared the floor: there is nothing to grade the error axis on.
func worstGradableErrorRate(stats []routeStat) (routeStat, bool) {
	var worst routeStat
	found := false
	for _, s := range stats {
		if s.ErrorSamples < minErrorSamples {
			continue
		}
		if !found || s.ErrorRate > worst.ErrorRate {
			worst, found = s, true
		}
	}
	return worst, found
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
	b.reason = ""
	b.updated = time.Now().UTC()
}

// markStale flags the view stale while preserving the last-known snapshot — the
// degrade-don't-crash behaviour: serve last-known flagged stale rather than
// dropping the panel.
func (b *Bucket) markStale(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale = true
	b.reason = goapi.Classify(err)
}

// throughputSignal renders the recent-window traffic for the bounded throughput
// trend: the request rate over the interval since the previous read, with the
// windowed count and route fan-out, so the trend reflects current load rather than
// a lifetime total that only ever climbs.
func throughputSignal(w window) core.Signal {
	return core.Signal{
		At:   w.at,
		Kind: "throughput",
		Text: fmt.Sprintf("%.1f req/s (%d in window across %d route(s))", w.perSecond, w.requests, len(w.latency.Routes)),
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
