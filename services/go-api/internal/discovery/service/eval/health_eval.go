package eval

import (
	"context"
	"sort"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/errgroup"
)

type HealthReport struct {
	Searches      int     `json:"searches"`
	Results       int     `json:"results"` // total result rows seen
	WithArtwork   int     `json:"with_artwork"`
	BridgedMerges int     `json:"bridged_merges"`
	FillRate      float64 `json:"fill_rate"`       // with_artwork / results
	BridgeHitRate float64 `json:"bridge_hit_rate"` // bridged_merges / results
	LatencyP50Ms  int64   `json:"latency_p50_ms"`
	LatencyP95Ms  int64   `json:"latency_p95_ms"`
	LatencyMaxMs  int64   `json:"latency_max_ms"`
}

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
						if domain.ResolutionTierFromExtras(r.Extras) == domain.EntityResolutionBridge {
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

func (r HealthReport) HealthMetrics() []NamedMetric {
	return []NamedMetric{
		{Name: "health.fill_rate", Value: r.FillRate, HigherIsBetter: true},
		{Name: "health.bridge_hit_rate", Value: r.BridgeHitRate, HigherIsBetter: true},
		{Name: "health.latency_p50_ms", Value: float64(r.LatencyP50Ms), HigherIsBetter: false},
		{Name: "health.latency_p95_ms", Value: float64(r.LatencyP95Ms), HigherIsBetter: false},
	}
}
