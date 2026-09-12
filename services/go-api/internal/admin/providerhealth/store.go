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

	statusOK          = "ok"
	statusRateLimited = "rate_limited"
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
	// now stamps samples with a monotonic-bearing instant; since measures
	// elapsed from it. Both are monotonic-safe (immune to wall-clock jumps)
	// in production and injectable so tests can simulate clock steps.
	now   func() time.Time
	since func(time.Time) time.Duration
}

func NewStore() *Store {
	return newStoreWithClock(time.Now, time.Since)
}

func newStoreWithClock(now func() time.Time, since func(time.Time) time.Duration) *Store {
	return &Store{
		samples: make(map[string][]sample),
		last:    make(map[string]string),
		now:     now,
		since:   since,
	}
}

func (s *Store) Record(provider, status string, latencyMs int64) {
	now := s.now()
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
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ProviderSnapshot, 0, len(s.samples))
	for provider, xs := range s.samples {
		kept := xs[:0]
		for _, x := range xs {
			if s.since(x.at) < window {
				kept = append(kept, x)
			}
		}
		s.samples[provider] = kept
		out = append(out, summarize(provider, s.last[provider], kept))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

func summarize(provider, current string, kept []sample) ProviderSnapshot {
	counts := make(map[string]int)
	var latencySum int64
	latencies := make([]int64, 0, len(kept))
	for _, x := range kept {
		counts[x.status]++
		latencySum += x.latencyMs
		latencies = append(latencies, x.latencyMs)
	}
	var avg int64
	if len(kept) > 0 {
		avg = latencySum / int64(len(kept))
	}
	var errs int
	for status, n := range counts {
		if status != statusOK {
			errs += n
		}
	}
	var errorRate float64
	if len(kept) > 0 {
		errorRate = float64(errs) / float64(len(kept))
	}
	return ProviderSnapshot{
		Provider:        provider,
		CurrentStatus:   current,
		CountsPerStatus: counts,
		TotalCalls:      len(kept),
		AvgLatencyMs:    avg,
		P95LatencyMs:    percentile(latencies, 0.95),
		ErrorRate:       errorRate,
		RateLimited:     counts[statusRateLimited],
	}
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
