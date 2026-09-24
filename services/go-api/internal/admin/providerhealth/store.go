package providerhealth

import (
	"altune/go-api/internal/discovery/domain"
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
	status    domain.ProviderStatus
	latencyMs int64
	at        time.Time
}

// Store is the windowed in-memory sample store behind the provider health
// view. It is safe for concurrent use: every path takes mu, so the provider
// call sites record from whichever goroutine made the call while the operator's
// reads run from another.
type Store struct {
	mu      sync.Mutex
	samples map[string][]sample
	last    map[string]domain.ProviderStatus
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
		last:    make(map[string]domain.ProviderStatus),
		now:     now,
		since:   since,
	}
}

func (s *Store) Record(providerName domain.ProviderName, status domain.ProviderStatus, latencyMs int64) {
	now := s.now()
	provider := providerName.String()
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
	Truncated       bool           `json:"truncated"`
}

func (s *Store) Snapshot() []ProviderSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ProviderSnapshot, 0, len(s.samples))
	for provider, xs := range s.samples {
		kept := s.within(xs)
		if len(kept) == 0 {
			s.forget(provider)
			continue
		}
		s.samples[provider] = kept
		out = append(out, summarize(provider, s.last[provider], kept))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// within drops the samples that have left the window. It compacts in place, so
// the caller must drop the slice it passed in.
func (s *Store) within(xs []sample) []sample {
	kept := xs[:0]
	for _, x := range xs {
		if s.since(x.at) < window {
			kept = append(kept, x)
		}
	}
	return kept
}

// forget releases a provider that has nothing live left to report, so an
// experiment or a retired provider cannot hold its slots for the life of the
// process.
func (s *Store) forget(provider string) {
	delete(s.samples, provider)
	delete(s.last, provider)
}

func summarize(provider string, current domain.ProviderStatus, kept []sample) ProviderSnapshot {
	counts := make(map[string]int)
	for _, x := range kept {
		counts[x.status.String()]++
	}
	avg, p95 := latencyStats(kept)
	return ProviderSnapshot{
		Provider:        provider,
		CurrentStatus:   current.String(),
		CountsPerStatus: counts,
		TotalCalls:      len(kept),
		AvgLatencyMs:    avg,
		P95LatencyMs:    p95,
		ErrorRate:       errorRate(counts, len(kept)),
		RateLimited:     counts[domain.ProviderStatusRateLimited.String()],
		Truncated:       isCapped(kept),
	}
}

// latencyStats returns the mean and 95th-percentile latency of the samples.
func latencyStats(kept []sample) (avg, p95 int64) {
	var sum int64
	latencies := make([]int64, 0, len(kept))
	for _, x := range kept {
		sum += x.latencyMs
		latencies = append(latencies, x.latencyMs)
	}
	if len(kept) > 0 {
		avg = sum / int64(len(kept))
	}
	return avg, percentile(latencies, 0.95)
}

// errorRate is the share of total calls whose status was anything but OK.
func errorRate(counts map[string]int, total int) float64 {
	if total == 0 {
		return 0
	}
	var errs int
	for status, n := range counts {
		if status != domain.ProviderStatusOK.String() {
			errs += n
		}
	}
	return float64(errs) / float64(total)
}

// isCapped reports whether the per-provider cap can have dropped calls that
// still belong to the window. Every other number in the snapshot then covers
// only the retained tail — a shorter span than window — and a reader must not
// take the total for the provider's true call count.
func isCapped(kept []sample) bool {
	return len(kept) >= perProviderCap
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
