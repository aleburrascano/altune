package service

// Health gauges — report-only (plan 2026-06-24-001, Phase 3).
//
// These are operational signals, NOT gated. They fluctuate with provider uptime
// (artwork resolves only when its sources are up; latency tracks load), so a
// threshold on them would flap — the exact false-red that gets a harness deleted.
// They are tracked in baselines.json for visibility and history only; one
// graduates to a gated metric ONLY if it proves it predicts user-visible
// breakage. Deliberately not a HarnessReport — the cmd prints them and never
// gates them.
//
//   - fill_rate       : share of returned results with artwork resolved (a result
//                       with no art is a near-dead card).
//   - bridge_hit_rate : share of results merged via the cross-provider identity
//                       bridge (resolution_tier == "bridge") — how much reach the
//                       bridge actually buys.
//   - latency p50/p95 : end-to-end search wall time.

import (
	"context"
	"sort"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/errgroup"
)

// HealthReport is the report-only operational snapshot.
type HealthReport struct {
	Searches       int     `json:"searches"`
	Results        int     `json:"results"`          // total result rows seen
	WithArtwork    int     `json:"with_artwork"`
	BridgedMerges  int     `json:"bridged_merges"`
	FillRate       float64 `json:"fill_rate"`        // with_artwork / results
	BridgeHitRate  float64 `json:"bridge_hit_rate"`  // bridged_merges / results
	LatencyP50Ms   int64   `json:"latency_p50_ms"`
	LatencyP95Ms   int64   `json:"latency_p95_ms"`
	LatencyMaxMs   int64   `json:"latency_max_ms"`
}

// RunHealthEval searches "artist title" for each entity, timing each call and
// tallying artwork fill and bridge-merge incidence across the returned rows.
func RunHealthEval(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency int, progress func(done, total int)) HealthReport {
	if concurrency < 1 {
		concurrency = 1
	}
	total := len(entities)
	step := total / 20
	if step < 1 {
		step = 1
	}

	var (
		mu        sync.Mutex
		results   int
		artwork   int
		bridged   int
		latencies []int64
		done      int
	)

	g := new(errgroup.Group)
	g.SetLimit(concurrency)
	for _, entity := range entities {
		entity := entity
		g.Go(func() error {
			if entity.Artist != "" {
				query := entity.Artist + " " + entity.Title
				start := time.Now()
				shown, err := searcher.Search(ctx, query)
				ms := time.Since(start).Milliseconds()
				if err == nil {
					mu.Lock()
					latencies = append(latencies, ms)
					for _, r := range shown {
						results++
						if r.ImageURL != "" {
							artwork++
						}
						if stringExtra(r, "resolution_tier") == domain.EntityResolutionBridge.String() {
							bridged++
						}
					}
					mu.Unlock()
				}
			}
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait()

	report := HealthReport{
		Searches:      len(latencies),
		Results:       results,
		WithArtwork:   artwork,
		BridgedMerges: bridged,
	}
	if results > 0 {
		report.FillRate = float64(artwork) / float64(results)
		report.BridgeHitRate = float64(bridged) / float64(results)
	}
	report.LatencyP50Ms = percentile(latencies, 50)
	report.LatencyP95Ms = percentile(latencies, 95)
	report.LatencyMaxMs = percentile(latencies, 100)
	return report
}

// percentile returns the p-th percentile (0–100) of the samples via
// nearest-rank, or 0 for an empty set.
func percentile(samples []int64, p int) int64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64{}, samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (p * len(sorted)) / 100
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// HealthMetrics renders the gauges as NamedMetrics for recording in baselines
// (visibility/history) — never gated. Latency direction is lower-is-better;
// fill/bridge are higher-is-better, but none of these flip an exit code.
func (r HealthReport) HealthMetrics() []NamedMetric {
	return []NamedMetric{
		{Name: "health.fill_rate", Value: r.FillRate, HigherIsBetter: true},
		{Name: "health.bridge_hit_rate", Value: r.BridgeHitRate, HigherIsBetter: true},
		{Name: "health.latency_p50_ms", Value: float64(r.LatencyP50Ms), HigherIsBetter: false},
		{Name: "health.latency_p95_ms", Value: float64(r.LatencyP95Ms), HigherIsBetter: false},
	}
}
