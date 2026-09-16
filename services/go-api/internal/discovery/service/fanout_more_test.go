package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type slowTimeoutProvider struct {
	fakeProvider
	timeout time.Duration
}

func (p *slowTimeoutProvider) SearchTimeout() time.Duration { return p.timeout }

func TestFanOut_PerProviderTimeoutOverride(t *testing.T) {
	slow := &slowTimeoutProvider{
		fakeProvider: fakeProvider{
			name: domain.ProviderITunes, delay: 80 * time.Millisecond,
			results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 70)},
		},
		timeout: 20 * time.Millisecond,
	}
	normal := &fakeProvider{
		name: domain.ProviderDeezer, delay: 80 * time.Millisecond,
		results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)},
	}
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
	skipped := &countingProvider{
		name:    domain.ProviderITunes,
		results: []domain.SearchResult{deezerTrack("Humble", "x", 10)},
	}
	good := &fakeProvider{
		name:    domain.ProviderDeezer,
		results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)},
	}
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
	names := []domain.ProviderName{
		domain.ProviderDeezer, domain.ProviderITunes, domain.ProviderMusicBrainz,
		domain.ProviderSoundCloud, domain.ProviderLastFM, domain.ProviderSpotify,
		domain.ProviderAppleMusic, domain.ProviderYouTube,
	}
	providers := make([]ports.SearchProvider, len(names))
	for i, n := range names {
		providers[i] = &fakeProvider{
			name:  n,
			delay: time.Duration(len(names)-i) * 10 * time.Millisecond,
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

func breakerFailures(cb *CircuitBreaker, provider domain.ProviderName) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if entry := cb.circuits[provider]; entry != nil {
		return entry.failures
	}
	return 0
}

var errQueueShed = fmt.Errorf("%w: %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

// Search settles through the same health classification as content calls.
func TestFanOut_BreakerCountsOnlyHealthFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"rate-limit queue timeout", fmt.Errorf("mb: all kinds failed: %w", errQueueShed), 0},
		{"parse error", errors.New("invalid character '<'"), 0},
		{"404", statusErr(404), 0},
		{"503", statusErr(503), 1},
		{"429", statusErr(429), 1},
		{"transport", upstreamDown, 1},
		{"deadline", context.DeadlineExceeded, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := NewCircuitBreaker()
			svc := NewService([]ports.SearchProvider{&fakeProvider{name: domain.ProviderITunes, err: tc.err}}, cb)

			_, statuses := svc.fanOut(context.Background(), "humble", nil)

			if got := breakerFailures(cb, domain.ProviderITunes); got != tc.want {
				t.Errorf("recorded failures = %d, want %d", got, tc.want)
			}
			if statuses[0].Status == domain.ProviderStatusOK {
				t.Errorf("status = ok for a failed search")
			}
		})
	}
}

// A queue-shed search reports a timeout, like one its budget cut off.
func TestFanOut_QueueShedReportsTimeout(t *testing.T) {
	svc := NewService([]ports.SearchProvider{&fakeProvider{name: domain.ProviderITunes, err: errQueueShed}}, NewCircuitBreaker())

	_, statuses := svc.fanOut(context.Background(), "humble", nil)

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout", statuses[0].Status)
	}
}

// A queue-shed half-open probe hands its slot back instead of re-opening.
func TestFanOut_QueueShedProbeReleasesSlot(t *testing.T) {
	cb := NewCircuitBreaker()
	tripToHalfOpenWindow(cb, domain.ProviderITunes)
	svc := NewService([]ports.SearchProvider{&fakeProvider{name: domain.ProviderITunes, err: errQueueShed}}, cb)

	_, statuses := svc.fanOut(context.Background(), "humble", nil)

	if statuses[0].Status == domain.ProviderStatusCircuitOpen {
		t.Fatalf("setup: probe not admitted")
	}
	if !cb.AllowRequest(domain.ProviderITunes) {
		t.Errorf("next request refused: the shed probe held its slot or re-opened the circuit")
	}
}

// ignoresCtxProvider outlives its budget and then reports an error that does
// not itself say "timeout", as a killed subprocess would.
type ignoresCtxProvider struct{ fakeProvider }

func (p *ignoresCtxProvider) SearchTimeout() time.Duration { return 5 * time.Millisecond }

func (p *ignoresCtxProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	<-ctx.Done()
	return nil, errors.New("signal: killed")
}

func TestFanOut_BudgetCutOffUnclassifiedErrorStillCounts(t *testing.T) {
	cb := NewCircuitBreaker()
	svc := NewService([]ports.SearchProvider{&ignoresCtxProvider{fakeProvider{name: domain.ProviderITunes}}}, cb)

	svc.fanOut(context.Background(), "humble", nil)

	if got := breakerFailures(cb, domain.ProviderITunes); got != 1 {
		t.Errorf("recorded failures = %d, want 1 for a search cut off by its own budget", got)
	}
}
