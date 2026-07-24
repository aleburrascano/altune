package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// slowTimeoutProvider is a fakeProvider that also implements the optional
// SearchTimeout override the fan-out honors per-provider.
type slowTimeoutProvider struct {
	fakeProvider
	timeout time.Duration
}

func (p *slowTimeoutProvider) SearchTimeout() time.Duration { return p.timeout }

func TestFanOut_PerProviderTimeoutOverride(t *testing.T) {
	// Both providers delay 80ms. The overriding provider allows itself only 20ms
	// (times out); the default-timeout provider (1500ms) succeeds. Proves the
	// override applies to the declaring provider alone.
	slow := &slowTimeoutProvider{
		fakeProvider: fakeProvider{name: domain.ProviderITunes, delay: 80 * time.Millisecond,
			results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 70)}},
		timeout: 20 * time.Millisecond,
	}
	normal := &fakeProvider{name: domain.ProviderDeezer, delay: 80 * time.Millisecond,
		results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{slow, normal}, NewCircuitBreaker())

	perProvider, statuses := svc.fanOut(context.Background(), "humble", nil)

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("overriding provider status = %v, want timeout (its own 20ms budget fired)", statuses[0].Status)
	}
	if statuses[1].Status != domain.ProviderStatusOK {
		t.Errorf("default provider status = %v, want ok (default budget untouched)", statuses[1].Status)
	}
	if len(perProvider) != 1 {
		t.Errorf("perProvider groups = %d, want 1 (only the surviving provider)", len(perProvider))
	}
}

func TestFanOut_TimeoutRecordsBreakerFailure(t *testing.T) {
	// A per-provider timeout IS a provider failure (unlike parent cancellation):
	// the breaker must count it.
	slow := &slowTimeoutProvider{
		fakeProvider: fakeProvider{name: domain.ProviderITunes, delay: 80 * time.Millisecond},
		timeout:      10 * time.Millisecond,
	}
	cb := NewCircuitBreaker()
	svc := NewService([]ports.SearchProvider{slow}, cb)

	svc.fanOut(context.Background(), "humble", nil)

	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderITunes]
	cb.mu.Unlock()
	if entry == nil || entry.failures != 1 {
		t.Errorf("breaker entry = %+v, want 1 recorded failure for the timeout", entry)
	}
}

func TestFanOut_OpenBreakerSkipsProviderEntirely(t *testing.T) {
	skipped := &countingProvider{name: domain.ProviderITunes,
		results: []domain.SearchResult{deezerTrack("Humble", "x", 10)}}
	good := &fakeProvider{name: domain.ProviderDeezer,
		results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	cb := NewCircuitBreaker()
	for i := 0; i < failureThreshold; i++ {
		cb.RecordFailure(domain.ProviderITunes)
	}
	svc := NewService([]ports.SearchProvider{skipped, good}, cb)

	perProvider, statuses := svc.fanOut(context.Background(), "humble", nil)

	if skipped.calls != 0 {
		t.Errorf("open-breaker provider was called %d times, want 0", skipped.calls)
	}
	if statuses[0].Status != domain.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", statuses[0].Status)
	}
	if statuses[1].Status != domain.ProviderStatusOK {
		t.Errorf("healthy provider status = %v, want ok", statuses[1].Status)
	}
	if len(perProvider) != 1 {
		t.Errorf("perProvider groups = %d, want 1", len(perProvider))
	}
}

func TestFanOut_ManyProvidersDeterministicSlotOrder(t *testing.T) {
	// Eight providers finishing in REVERSE-staggered order: the last-listed
	// provider completes first. Both outputs must still follow the fixed provider
	// order (each goroutine writes only its own slot), never completion order —
	// that determinism is what keeps tied ranks stable run-to-run.
	names := []domain.ProviderName{
		domain.ProviderDeezer, domain.ProviderITunes, domain.ProviderMusicBrainz,
		domain.ProviderSoundCloud, domain.ProviderLastFM, domain.ProviderSpotify,
		domain.ProviderAppleMusic, domain.ProviderYouTube,
	}
	providers := make([]ports.SearchProvider, len(names))
	for i, n := range names {
		providers[i] = &fakeProvider{
			name:  n,
			delay: time.Duration(len(names)-i) * 10 * time.Millisecond, // slot 0 slowest
			results: []domain.SearchResult{
				deezerTrack(fmt.Sprintf("Humble %d", i), n.String(), float64(50+i)),
			},
		}
	}
	svc := NewService(providers, NewCircuitBreaker())

	perProvider, statuses := svc.fanOut(context.Background(), "humble", nil)

	if len(statuses) != len(names) || len(perProvider) != len(names) {
		t.Fatalf("statuses=%d groups=%d, want %d each", len(statuses), len(perProvider), len(names))
	}
	for i, n := range names {
		if statuses[i].Provider != n {
			t.Errorf("statuses[%d].Provider = %v, want %v (fixed order, not completion order)", i, statuses[i].Provider, n)
		}
		wantTitle := fmt.Sprintf("Humble %d", i)
		if perProvider[i][0].Title != wantTitle {
			t.Errorf("perProvider[%d] = %q, want %q", i, perProvider[i][0].Title, wantTitle)
		}
	}
}
