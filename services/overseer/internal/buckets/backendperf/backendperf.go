package backendperf

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const historyCapacity = 120

const (
	bucketID           = "backendperf"
	seriesThroughput   = "throughput_rps"
	seriesErrorRate    = "error_rate"
	seriesP50MS        = "p50_ms"
	seriesP95MS        = "p95_ms"
	seriesP99MS        = "p99_ms"
	perRouteSeriesStem = "p95_ms:"
	maxPerRouteSeries  = 20
)

const (
	warnP99Ms     = 100.0
	criticalP99Ms = 500.0
)

const (
	warnErrorRate     = 0.01
	criticalErrorRate = 0.05
)

const minErrorSamples = 10

var errUnconfigured = errors.New("backendperf: go-api not configured")

type metricsReader interface {
	AdminMetricsLive(ctx context.Context) (goapi.LiveMetrics, error)
}

type Bucket struct {
	reader  metricsReader
	history core.Store
	series  core.Series

	mu      sync.RWMutex
	last    goapi.LatencyMetrics
	have    bool
	stale   bool
	reason  string
	updated time.Time

	prev     goapi.LatencyMetrics
	prevAt   time.Time
	havePrev bool
}

func New() *Bucket { return newBucket(readerFromEnv()) }

func newBucket(reader metricsReader) *Bucket {
	return &Bucket{
		reader:  reader,
		history: core.NewRingStore(historyCapacity),
		series:  discardSeries{},
	}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Back-end performance"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesP95MS
}

func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	live, err := b.reader.AdminMetricsLive(ctx)
	if err != nil {
		b.markStale(err)
		return nil, fmt.Errorf("backendperf: live metrics unreachable: %w", err)
	}
	w := b.advanceWindow(live.Latency, time.Now())
	stats := routeStats(w.latency)
	b.recordSeries(w, stats)
	b.recordFresh(w.latency)
	return []core.Signal{throughputSignal(w)}, nil
}

func (b *Bucket) recordSeries(w window, stats []routeStat) {
	agg := aggregateRoutes(w.latency.Routes)
	at := w.at
	b.series.Record(bucketID, seriesThroughput, core.Point{At: at, Value: w.perSecond})
	b.series.Record(bucketID, seriesErrorRate, core.Point{At: at, Value: errorRate(agg.Status)})
	b.series.Record(bucketID, seriesP50MS, core.Point{At: at, Value: estimatePercentile(agg.Buckets, 0.50).Ms})
	b.series.Record(bucketID, seriesP95MS, core.Point{At: at, Value: estimatePercentile(agg.Buckets, 0.95).Ms})
	b.series.Record(bucketID, seriesP99MS, core.Point{At: at, Value: estimatePercentile(agg.Buckets, 0.99).Ms})
	for _, s := range topRoutesByTraffic(stats, maxPerRouteSeries) {
		b.series.Record(bucketID, perRouteSeriesStem+s.Route, core.Point{At: at, Value: s.P95.Ms})
	}
}

func topRoutesByTraffic(stats []routeStat, limit int) []routeStat {
	top := append([]routeStat(nil), stats...)
	sort.SliceStable(top, func(i, j int) bool {
		if top[i].Count != top[j].Count {
			return top[i].Count > top[j].Count
		}
		return top[i].Route < top[j].Route
	})
	if len(top) > limit {
		top = top[:limit]
	}
	return top
}

func aggregateRoutes(routes map[string]goapi.RouteLatency) goapi.RouteLatency {
	var agg goapi.RouteLatency
	index := make(map[string]int, len(agg.Buckets))
	for _, rl := range routes {
		agg.Count += rl.Count
		agg.SumMs += rl.SumMs
		agg.Status.Count2xx += rl.Status.Count2xx
		agg.Status.Count4xx += rl.Status.Count4xx
		agg.Status.Count5xx += rl.Status.Count5xx
		for _, bkt := range rl.Buckets {
			if i, ok := index[bkt.LeMs]; ok {
				agg.Buckets[i].Count += bkt.Count
				continue
			}
			index[bkt.LeMs] = len(agg.Buckets)
			agg.Buckets = append(agg.Buckets, bkt)
		}
	}
	return agg
}

type window struct {
	latency   goapi.LatencyMetrics
	requests  uint64
	perSecond float64
	at        time.Time
}

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

func deltaStatus(prev, cur goapi.StatusClasses) goapi.StatusClasses {
	return goapi.StatusClasses{
		Count2xx: monotonicDelta(prev.Count2xx, cur.Count2xx),
		Count4xx: monotonicDelta(prev.Count4xx, cur.Count4xx),
		Count5xx: monotonicDelta(prev.Count5xx, cur.Count5xx),
	}
}

func monotonicDelta(prev, cur uint64) uint64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}

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

type Data struct {
	Routes     []routeStat   `json:"routes"`
	Throughput []core.Signal `json:"throughput"`
}

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

func (b *Bucket) recordFresh(m goapi.LatencyMetrics) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last = m
	b.have = true
	b.stale = false
	b.reason = ""
	b.updated = time.Now().UTC()
}

func (b *Bucket) markStale(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale = true
	b.reason = goapi.Classify(err)
}

func throughputSignal(w window) core.Signal {
	return core.Signal{
		At:   w.at,
		Kind: "throughput",
		Text: fmt.Sprintf("%.1f req/s (%d in window across %d route(s))", w.perSecond, w.requests, len(w.latency.Routes)),
	}
}

func readerFromEnv() metricsReader {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullReader{}
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("backendperf: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return nullReader{}
	}
	return c
}

type nullReader struct{}

func (nullReader) AdminMetricsLive(context.Context) (goapi.LiveMetrics, error) {
	return goapi.LiveMetrics{}, &goapi.SourceDownError{Op: "GET /observe/metrics/live", Err: errUnconfigured}
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
