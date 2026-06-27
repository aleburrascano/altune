// Package providerhealth holds the in-memory rolling-window store behind the
// Mission Control provider status board. It aggregates per-provider scatter-
// gather outcomes (ProviderStatus) that the discovery handler already computes —
// no change to the ranking path, and no external metrics store.
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

// Store is concurrency-safe. The discovery handler records into it after each
// search; the operator board reads snapshots.
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

// Record adds one provider outcome. Called off the ranking path (at the search
// response boundary), so it never affects search latency or correctness.
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

// ProviderSnapshot is one provider's status board row.
type ProviderSnapshot struct {
	Provider     string         `json:"provider"`
	Current      string         `json:"current"`     // most recent status
	Counts       map[string]int `json:"counts"`      // per-status counts in the window
	Total        int            `json:"total"`       // total calls in the window
	AvgLatencyMs int64          `json:"avg_latency_ms"`
	P95LatencyMs int64          `json:"p95_latency_ms"`
	ErrorRate    float64        `json:"error_rate"`     // non-ok outcomes / total, in the window
	RateLimited  int            `json:"rate_limited"`   // rate_limited outcomes in the window (quota-pressure signal)
}

// Snapshot returns each provider's rolling status mix, ordered by name. It prunes
// stale samples as it reads.
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
			Provider:     provider,
			Current:      s.last[provider],
			Counts:       counts,
			Total:        len(kept),
			AvgLatencyMs: avg,
			P95LatencyMs: percentile(latencies, 0.95),
			ErrorRate:    errorRate,
			RateLimited:  counts["rate_limited"],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// percentile returns the nearest-rank p-th percentile latency (p in [0,1]), or 0
// for an empty set. Sorts a copy so the caller's slice is untouched.
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
