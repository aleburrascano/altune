package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type slowTimeoutProvider struct {
	fakeProvider
	timeout time.Duration
}

func (p *slowTimeoutProvider) SearchTimeout() time.Duration { return p.timeout }

func TestFanOut_PerProviderTimeoutOverride(t *testing.T) {
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
		cb.RecordFailure(context.Background(), domain.ProviderITunes)
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

func TestFanOut_QueueShedReportsTimeout(t *testing.T) {
	svc := NewService([]ports.SearchProvider{&fakeProvider{name: domain.ProviderITunes, err: errQueueShed}}, NewCircuitBreaker())

	_, statuses := svc.fanOut(context.Background(), "humble", nil)

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout", statuses[0].Status)
	}
}

func TestFanOut_QueueShedProbeReleasesSlot(t *testing.T) {
	cb := NewCircuitBreaker()
	tripToHalfOpenWindow(cb, domain.ProviderITunes)
	svc := NewService([]ports.SearchProvider{&fakeProvider{name: domain.ProviderITunes, err: errQueueShed}}, cb)

	_, statuses := svc.fanOut(context.Background(), "humble", nil)

	if statuses[0].Status == domain.ProviderStatusCircuitOpen {
		t.Fatalf("setup: probe not admitted")
	}
	if !cb.AllowRequest(context.Background(), domain.ProviderITunes) {
		t.Errorf("next request refused: the shed probe held its slot or re-opened the circuit")
	}
}

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

type partialKindsProvider struct {
	countingProvider
}

func (p *partialKindsProvider) Search(ctx context.Context, q string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	res, _ := p.countingProvider.Search(ctx, q, kinds)
	return res, &domain.PartialKindsError{Failed: []domain.ResultKind{domain.ResultKindAlbum}, Err: context.DeadlineExceeded}
}

func TestFanOut_PartialKindsKeepResultsButAreNeverCached(t *testing.T) {
	p := &partialKindsProvider{countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}}
	breaker := NewCircuitBreaker()
	slates := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, breaker,
		WithResultCache(slates), WithProviderResultCache(newFakeProviderCache()))

	out := runSearch(t, svc, "humble")
	runSearch(t, svc, "humble")

	if len(out.Results) != 1 {
		t.Fatalf("results = %v, want the kinds that answered kept", titles(out.Results))
	}
	if !out.Partial {
		t.Error("a missing kind must mark the search partial")
	}
	if got := out.ProviderStatuses[0].Status; got != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (the album search timed out)", got)
	}
	if slates.sets != 0 || p.calls != 2 {
		t.Errorf("a partial answer must not be cached: slate sets=%d provider calls=%d", slates.sets, p.calls)
	}
	if n := breakerFailures(breaker, domain.ProviderDeezer); n != 0 {
		t.Errorf("breaker failures = %d, want 0 (the provider answered)", n)
	}
}

type lateProvider struct {
	name      domain.ProviderName
	results   []domain.SearchResult
	release   chan struct{}
	started   chan struct{}
	cancelled chan struct{}
	calls     atomic.Int32
}

func newLateProvider(name domain.ProviderName, results ...domain.SearchResult) *lateProvider {
	return &lateProvider{
		name:      name,
		results:   results,
		release:   make(chan struct{}),
		started:   make(chan struct{}, 8),
		cancelled: make(chan struct{}, 8),
	}
}

func (p *lateProvider) Name() domain.ProviderName { return p.name }

func (p *lateProvider) SupportedKinds() map[domain.ResultKind]bool { return domain.AllKinds() }

func (p *lateProvider) SearchTimeout() time.Duration { return 5 * time.Second }

func (p *lateProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	p.calls.Add(1)
	p.started <- struct{}{}
	select {
	case <-p.release:
		return p.results, nil
	case <-ctx.Done():
		p.cancelled <- struct{}{}
		return nil, ctx.Err()
	}
}

func statusOf(out *SearchOutput, provider domain.ProviderName) domain.ProviderSearchResponse {
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	return domain.ProviderSearchResponse{}
}

func TestDeadline_AnswersWithoutTheSlowProvider(t *testing.T) {
	fast := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(domain.ProviderSpotify, deezerTrack("Humble", "Skrillex", 70))
	slates := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{fast, slow}, NewCircuitBreaker(),
		WithSearchDeadline(20*time.Millisecond), WithResultCache(slates))
	t.Cleanup(func() { close(slow.release); svc.WaitForBackground() })

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %v, want only the fast provider's answer", titles(out.Results))
	}
	if st := statusOf(out, domain.ProviderSpotify).Status; st != domain.ProviderStatusPending || st.String() != "pending" {
		t.Errorf("slow provider status = %v, want pending", st)
	}
	if out.Partial {
		t.Error("a provider still answering is not a failure: the search must not be partial")
	}
	if slates.sets != 0 {
		t.Errorf("a slate missing a pending provider must not be cached, sets = %d", slates.sets)
	}
}

func TestDeadline_LateAnswerServesTheNextSearch(t *testing.T) {
	fast := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(domain.ProviderSpotify, deezerTrack("Humble", "Skrillex", 70))
	slates := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{fast, slow}, NewCircuitBreaker(),
		WithSearchDeadline(20*time.Millisecond), WithResultCache(slates), WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	close(slow.release)
	svc.WaitForBackground()
	out := runSearch(t, svc, "humble")

	if got := slow.calls.Load(); got != 1 {
		t.Fatalf("slow provider calls = %d, want 1 (its late answer was cached and reused)", got)
	}
	if len(out.Results) != 2 || out.Partial {
		t.Fatalf("second search: results=%v partial=%v, want both answers and not partial", titles(out.Results), out.Partial)
	}
	if slates.sets != 1 {
		t.Errorf("a slate with every provider answered must be cached, sets = %d", slates.sets)
	}
}

func TestDeadline_WaitsPastItUntilSomethingArrives(t *testing.T) {
	slow := newLateProvider(domain.ProviderSpotify, deezerTrack("Humble", "Skrillex", 70))
	svc := NewService([]ports.SearchProvider{slow}, NewCircuitBreaker(), WithSearchDeadline(time.Millisecond))
	done := make(chan *SearchOutput, 1)
	go func() { done <- runSearch(t, svc, "humble") }()
	waitFor(t, slow.started, "the provider call to start")

	select {
	case <-done:
		t.Fatal("the search answered with nothing while a provider was still working")
	case <-time.After(50 * time.Millisecond):
	}
	close(slow.release)

	select {
	case out := <-done:
		if len(out.Results) != 1 {
			t.Fatalf("results = %v, want the first answer that arrived", titles(out.Results))
		}
	case <-time.After(time.Second):
		t.Fatal("search did not answer once the provider did")
	}
}

func TestDeadline_CallerGoneBeforeTheAnswerStopsTheProviders(t *testing.T) {
	slow := newLateProvider(domain.ProviderSpotify)
	svc := NewService([]ports.SearchProvider{slow}, NewCircuitBreaker(), WithSearchDeadline(time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = svc.Execute(ctx, newUser(), newQuery(t, "dna"), false) }()
	waitFor(t, slow.started, "the provider call to start")

	cancel()

	waitFor(t, slow.cancelled, "the abandoned search's provider call to stop")
	svc.WaitForBackground()
}
