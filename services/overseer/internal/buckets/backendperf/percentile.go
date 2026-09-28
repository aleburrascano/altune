package backendperf

import (
	"altune/overseer/internal/goapi"
	"fmt"
	"math"
	"sort"
	"strconv"
)

type routeStat struct {
	Route        string     `json:"route"`
	Count        uint64     `json:"count"`
	ErrorRate    float64    `json:"error_rate"`
	ErrorSamples uint64     `json:"error_samples"`
	P50          percentile `json:"p50"`
	P95          percentile `json:"p95"`
	P99          percentile `json:"p99"`
}

type percentile struct {
	Ms       float64 `json:"ms"`
	Overflow bool    `json:"overflow"`
}

func formatMs(p percentile) string {
	if p.Overflow {
		return fmt.Sprintf("≥%.0f ms", p.Ms)
	}
	return fmt.Sprintf("%.0f ms", p.Ms)
}

func formatRate(r float64) string {
	return fmt.Sprintf("%.1f%%", r*100)
}

func routeStats(m goapi.LatencyMetrics) []routeStat {
	stats := make([]routeStat, 0, len(m.Routes))
	for route, rl := range m.Routes {
		if bucketTotal(rl.Buckets) == 0 {
			continue
		}
		stats = append(stats, statFor(route, rl))
	}
	sortSlowestFirst(stats)
	return stats
}

func statFor(route string, rl goapi.RouteLatency) routeStat {
	return routeStat{
		Route:        route,
		Count:        rl.Count,
		ErrorRate:    errorRate(rl.Status),
		ErrorSamples: classifiedResponses(rl.Status),
		P50:          estimatePercentile(rl.Buckets, 0.50),
		P95:          estimatePercentile(rl.Buckets, 0.95),
		P99:          estimatePercentile(rl.Buckets, 0.99),
	}
}

func errorRate(s goapi.StatusClasses) float64 {
	classified := classifiedResponses(s)
	if classified == 0 {
		return 0
	}
	return float64(s.Count5xx) / float64(classified)
}

func classifiedResponses(s goapi.StatusClasses) uint64 {
	return s.Count2xx + s.Count4xx + s.Count5xx
}

func sortSlowestFirst(stats []routeStat) {
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.P99.Ms != b.P99.Ms {
			return a.P99.Ms > b.P99.Ms
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Route < b.Route
	})
}

func estimatePercentile(buckets []goapi.LatencyBucket, p float64) percentile {
	total := bucketTotal(buckets)
	if total == 0 {
		return percentile{}
	}
	rank := p * float64(total)
	var cum float64
	lower := 0.0
	for _, b := range buckets {
		upper := bucketBound(b.LeMs)
		if b.Count > 0 && cum+float64(b.Count) >= rank {
			return interpolate(lower, upper, cum, float64(b.Count), rank)
		}
		cum += float64(b.Count)
		if !math.IsInf(upper, 1) {
			lower = upper
		}
	}
	return percentile{Ms: lower, Overflow: true}
}

func interpolate(lower, upper, cum, count, rank float64) percentile {
	if math.IsInf(upper, 1) {
		return percentile{Ms: lower, Overflow: true}
	}
	frac := (rank - cum) / count
	return percentile{Ms: lower + frac*(upper-lower)}
}

func bucketTotal(buckets []goapi.LatencyBucket) uint64 {
	var total uint64
	for _, b := range buckets {
		total += b.Count
	}
	return total
}

func bucketBound(label string) float64 {
	v, err := strconv.ParseFloat(label, 64)
	if err != nil || math.IsNaN(v) {
		return math.Inf(1)
	}
	return v
}
