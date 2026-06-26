// Package providerhealth holds the in-memory rolling-window store behind the
// Mission Control provider status board. It aggregates per-provider scatter-
// gather outcomes (ProviderStatus) that the discovery handler already computes —
// no change to the ranking path, and no external metrics store.
package providerhealth

import (
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
		for _, x := range xs {
			if x.at.After(cutoff) {
				kept = append(kept, x)
				counts[x.status]++
				latencySum += x.latencyMs
			}
		}
		s.samples[provider] = kept

		var avg int64
		if len(kept) > 0 {
			avg = latencySum / int64(len(kept))
		}
		out = append(out, ProviderSnapshot{
			Provider:     provider,
			Current:      s.last[provider],
			Counts:       counts,
			Total:        len(kept),
			AvgLatencyMs: avg,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}
