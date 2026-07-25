package providerhealth

import (
	"math"
	"sort"
	"sync"
	"time"
)

const (
	window         = 5 * time.Minute
	perProviderCap = 2048
)

type sample struct {
	status    string
	latencyMs int64
	at        time.Time
}

type Store struct {
	mu      sync.Mutex
	samples map[string][]sample
	last    map[string]string
}

func NewStore() *Store {
	return &Store{
		samples: make(map[string][]sample),
		last:    make(map[string]string),
	}
}

func (s *Store) Record(provider, status string, latencyMs int64) {
	now := time.Now().UTC()
	s.mu.Lock()
	xs := append(s.samples[provider], sample{status: status, latencyMs: latencyMs, at: now})
	if len(xs) > perProviderCap {
		xs = xs[len(xs)-perProviderCap:]
	}
	s.samples[provider] = xs
	s.last[provider] = status
	s.mu.Unlock()
}

type ProviderSnapshot struct {
	Provider        string         `json:"provider"`
	CurrentStatus   string         `json:"current"`
	CountsPerStatus map[string]int `json:"counts"`
	TotalCalls      int            `json:"total"`
	AvgLatencyMs    int64          `json:"avg_latency_ms"`
	P95LatencyMs    int64          `json:"p95_latency_ms"`
	ErrorRate       float64        `json:"error_rate"`
	RateLimited     int            `json:"rate_limited"`
}

func (s *Store) Snapshot() []ProviderSnapshot {
	cutoff := time.Now().UTC().Add(-window)
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ProviderSnapshot, 0, len(s.samples))
	for provider, xs := range s.samples {
		kept := xs[:0]
		counts := make(map[string]int)
		var latencySum int64
		latencies := make([]int64, 0, len(xs))
		for _, x := range xs {
			if x.at.After(cutoff) {
				kept = append(kept, x)
				counts[x.status]++
				latencySum += x.latencyMs
				latencies = append(latencies, x.latencyMs)
			}
		}
		s.samples[provider] = kept

		var avg int64
		if len(kept) > 0 {
			avg = latencySum / int64(len(kept))
		}
		var errs int
		for status, n := range counts {
			if status != "ok" {
				errs += n
			}
		}
		var errorRate float64
		if len(kept) > 0 {
			errorRate = float64(errs) / float64(len(kept))
		}
		out = append(out, ProviderSnapshot{
			Provider:        provider,
			CurrentStatus:   s.last[provider],
			CountsPerStatus: counts,
			TotalCalls:      len(kept),
			AvgLatencyMs:    avg,
			P95LatencyMs:    percentile(latencies, 0.95),
			ErrorRate:       errorRate,
			RateLimited:     counts["rate_limited"],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

func percentile(latencies []int64, p float64) int64 {
	if len(latencies) == 0 {
		return 0
	}
	xs := make([]int64, len(latencies))
	copy(xs, latencies)
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	idx := int(math.Ceil(p*float64(len(xs)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(xs) {
		idx = len(xs) - 1
	}
	return xs[idx]
}
