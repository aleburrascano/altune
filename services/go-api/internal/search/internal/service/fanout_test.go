package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/timingtest"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type slowTimeoutProvider struct {
	memory.FakeProvider
	timeout time.Duration
}

func (p *slowTimeoutProvider) timeouts() service.Option {
	return service.WithProviderTimeouts(map[domain.ProviderName]time.Duration{p.Provider: p.timeout})
}

var lateProviderTimeouts = service.WithProviderTimeouts(map[domain.ProviderName]time.Duration{domain.ProviderSpotify: 5 * time.Second})

func TestFanOut_PerProviderTimeoutOverride(t *testing.T) {
	bg := memory.NewTracker(t)
	slow := &slowTimeoutProvider{
		FakeProvider: memory.FakeProvider{
			Provider: domain.ProviderITunes, Delay: 80 * time.Millisecond,
			Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 70)},
		},
		timeout: 20 * time.Millisecond,
	}
	normal := &memory.FakeProvider{
		Provider: domain.ProviderDeezer, Delay: 80 * time.Millisecond,
		Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{slow, normal}, service.NewCircuitBreaker(clock.Now), slow.timeouts())

	results, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("overriding provider status = %v, want timeout (its own 20ms budget fired)", statuses[0].Status)
	}
	if statuses[1].Status != domain.ProviderStatusOK {
		t.Errorf("default provider status = %v, want ok (default budget untouched)", statuses[1].Status)
	}
	if len(results) != 1 || results[0].Popularity != 80 {
		t.Errorf("results = %v, want only the surviving provider's track", memory.Titles(results))
	}
}

func TestFanOut_TimeoutRecordsBreakerFailure(t *testing.T) {
	bg := memory.NewTracker(t)
	slow := &slowTimeoutProvider{
		FakeProvider: memory.FakeProvider{Provider: domain.ProviderITunes, Delay: 80 * time.Millisecond},
		timeout:      10 * time.Millisecond,
	}
	cb := service.NewCircuitBreaker(clock.Now)
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{slow}, cb, slow.timeouts())

	inspect(t, svc, context.Background())

	entry := cb.Snapshot(domain.ProviderITunes)
	if entry.Failures != 1 {
		t.Errorf("breaker entry = %+v, want 1 recorded failure for the timeout", entry)
	}
}

func TestFanOut_OpenBreakerSkipsProviderEntirely(t *testing.T) {
	bg := memory.NewTracker(t)
	skipped := &memory.CountingProvider{
		Provider: domain.ProviderITunes,
		Results:  []domain.SearchResult{memory.DeezerTrack("Humble", "x", 10)},
	}
	good := &memory.FakeProvider{
		Provider: domain.ProviderDeezer,
		Results:  []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	cb := service.NewCircuitBreaker(clock.Now)
	openBreaker(t, cb, domain.ProviderITunes)
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{skipped, good}, cb)

	results, statuses := inspect(t, svc, context.Background())

	if skipped.Calls != 0 {
		t.Errorf("open-breaker provider was called %d times, want 0", skipped.Calls)
	}
	if statuses[0].Status != domain.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", statuses[0].Status)
	}
	if statuses[1].Status != domain.ProviderStatusOK {
		t.Errorf("healthy provider status = %v, want ok", statuses[1].Status)
	}
	if len(results) != 1 || results[0].Popularity != 80 {
		t.Errorf("results = %v, want only the healthy provider's track", memory.Titles(results))
	}
}

func TestFanOut_ManyProvidersDeterministicSlotOrder(t *testing.T) {
	bg := memory.NewTracker(t)
	names := []domain.ProviderName{
		domain.ProviderDeezer, domain.ProviderITunes, domain.ProviderMusicBrainz,
		domain.ProviderSoundCloud, domain.ProviderTheAudioDB, domain.ProviderSpotify,
		domain.ProviderAppleMusic, domain.ProviderYouTube,
	}
	providers := make([]ports.SearchProvider, len(names))
	for i, n := range names {
		providers[i] = &memory.FakeProvider{
			Provider: n,
			Delay:    time.Duration(len(names)-i) * 10 * time.Millisecond,
			Results: []domain.SearchResult{
				memory.DeezerTrack(fmt.Sprintf("Humble %d", i), n.String(), float64(50+i)),
			},
		}
	}
	svc := memory.NewSearchService(t, bg, providers, service.NewCircuitBreaker(clock.Now))

	results, statuses := inspect(t, svc, context.Background())

	if len(statuses) != len(names) || len(results) != len(names) {
		t.Fatalf("statuses=%d results=%d, want %d each", len(statuses), len(results), len(names))
	}
	for i, n := range names {
		if statuses[i].Provider != n {
			t.Errorf("statuses[%d].Provider = %v, want %v (fixed order, not completion order)", i, statuses[i].Provider, n)
		}
		if statuses[i].Status != domain.ProviderStatusOK {
			t.Errorf("statuses[%d].Status = %v, want ok", i, statuses[i].Status)
		}
	}
}

func breakerFailures(cb *service.CircuitBreaker, provider domain.ProviderName) int {
	return cb.Snapshot(provider).Failures
}

func inspect(t *testing.T, svc *service.SearchService, ctx context.Context) ([]domain.SearchResult, []domain.ProviderSearchResponse) {
	t.Helper()
	return svc.InspectSearchWithStatuses(ctx, memory.NewQuery(t, "humble"))
}

func openBreaker(t *testing.T, cb *service.CircuitBreaker, provider domain.ProviderName) {
	t.Helper()
	for range breakerTripsAfter {
		if cb.GetStatus(provider) == domain.ProviderStatusCircuitOpen {
			t.Fatalf("%s breaker opened before %d failures", provider, breakerTripsAfter)
		}
		cb.RecordFailure(context.Background(), provider, service.Admission{})
	}
	if cb.GetStatus(provider) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("%s breaker still closed after %d failures", provider, breakerTripsAfter)
	}
}

var errQueueShed = fmt.Errorf("%w: %w", domain.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

func TestFanOut_BreakerCountsOnlyHealthFailures(t *testing.T) {
	bg := memory.NewTracker(t)
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"rate-limit queue timeout", fmt.Errorf("mb: all kinds failed: %w", errQueueShed), 0},
		{"parse error", errors.New("invalid character '<'"), 0},
		{"404", memory.UpstreamStatusError(404), 0},
		{"503", memory.UpstreamStatusError(503), 1},
		{"429", memory.UpstreamStatusError(429), 1},
		{"transport", memory.ErrUpstreamDown, 1},
		{"deadline", context.DeadlineExceeded, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := service.NewCircuitBreaker(clock.Now)
			svc := memory.NewSearchService(t, bg, []ports.SearchProvider{&memory.FakeProvider{Provider: domain.ProviderITunes, Err: tc.err}}, cb)

			_, statuses := inspect(t, svc, context.Background())

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
	bg := memory.NewTracker(t)
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{&memory.FakeProvider{Provider: domain.ProviderITunes, Err: errQueueShed}}, service.NewCircuitBreaker(clock.Now))

	_, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout", statuses[0].Status)
	}
}

func TestFanOut_QueueShedProbeReleasesSlot(t *testing.T) {
	bg := memory.NewTracker(t)
	cb := service.NewCircuitBreaker(clock.Now)
	tripToHalfOpenWindow(t, cb, domain.ProviderITunes)
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{&memory.FakeProvider{Provider: domain.ProviderITunes, Err: errQueueShed}}, cb)

	_, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status == domain.ProviderStatusCircuitOpen {
		t.Fatalf("setup: probe not admitted")
	}
	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderITunes); !ok {
		t.Errorf("next request refused: the shed probe held its slot or re-opened the circuit")
	}
}

type ignoresCtxProvider struct{ memory.FakeProvider }

func (p *ignoresCtxProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	<-ctx.Done()
	return nil, errors.New("signal: killed")
}

func TestFanOut_BudgetCutOffUnclassifiedErrorStillCounts(t *testing.T) {
	bg := memory.NewTracker(t)
	cb := service.NewCircuitBreaker(clock.Now)
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{&ignoresCtxProvider{memory.FakeProvider{Provider: domain.ProviderITunes}}}, cb, service.WithProviderTimeouts(map[domain.ProviderName]time.Duration{domain.ProviderITunes: 5 * time.Millisecond}))

	inspect(t, svc, context.Background())

	if got := breakerFailures(cb, domain.ProviderITunes); got != 1 {
		t.Errorf("recorded failures = %d, want 1 for a search cut off by its own budget", got)
	}
}

type partialKindsProvider struct {
	memory.CountingProvider
}

func (p *partialKindsProvider) Search(ctx context.Context, q string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	res, _ := p.CountingProvider.Search(ctx, q, kinds)
	return res, &domain.PartialKindsError{Failed: []domain.ResultKind{domain.ResultKindAlbum}, Err: context.DeadlineExceeded}
}

func TestFanOut_PartialKindsKeepResultsButAreNeverCached(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &partialKindsProvider{memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}}
	breaker := service.NewCircuitBreaker(clock.Now)
	slates := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, breaker, func(c *service.Collaborators) { c.ResultCache = slates; c.ProviderCache = newFakeProviderCache() })

	out := runSearch(t, svc, "humble")
	runSearch(t, svc, "humble")

	if len(out.Results) != 1 {
		t.Fatalf("results = %v, want the kinds that answered kept", memory.Titles(out.Results))
	}
	if !out.Partial {
		t.Error("a missing kind must mark the search partial")
	}
	if got := out.ProviderStatuses[0].Status; got != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (the album search timed out)", got)
	}
	if slates.Sets != 0 || p.Calls != 2 {
		t.Errorf("a partial answer must not be cached: slate sets=%d provider calls=%d", slates.Sets, p.Calls)
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

func statusOf(out *service.SearchOutput, provider domain.ProviderName) domain.ProviderSearchResponse {
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	return domain.ProviderSearchResponse{}
}

func TestDeadline_AnswersWithoutTheSlowProvider(t *testing.T) {
	bg := memory.NewTracker(t)
	fast := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(domain.ProviderSpotify, memory.DeezerTrack("Humble", "Skrillex", 70))
	slates := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{fast, slow}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ResultCache = slates }, service.WithSearchDeadline(20*time.Millisecond), lateProviderTimeouts)
	t.Cleanup(func() { close(slow.release); bg.Wait() })

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %v, want only the fast provider's answer", memory.Titles(out.Results))
	}
	if st := statusOf(out, domain.ProviderSpotify).Status; st != domain.ProviderStatusPending || st.String() != "pending" {
		t.Errorf("slow provider status = %v, want pending", st)
	}
	if out.Partial {
		t.Error("a provider still answering is not a failure: the search must not be partial")
	}
	if slates.Sets != 0 {
		t.Errorf("a slate missing a pending provider must not be cached, sets = %d", slates.Sets)
	}
}

func TestDeadline_LateAnswerServesTheNextSearch(t *testing.T) {
	bg := memory.NewTracker(t)
	fast := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(domain.ProviderSpotify, memory.DeezerTrack("Humble", "Skrillex", 70))
	slates := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{fast, slow}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ResultCache = slates; c.ProviderCache = newFakeProviderCache() }, service.WithSearchDeadline(20*time.Millisecond), lateProviderTimeouts)

	runSearch(t, svc, "humble")
	close(slow.release)
	bg.Wait()
	out := runSearch(t, svc, "humble")

	if got := slow.calls.Load(); got != 1 {
		t.Fatalf("slow provider calls = %d, want 1 (its late answer was cached and reused)", got)
	}
	if len(out.Results) != 2 || out.Partial {
		t.Fatalf("second search: results=%v partial=%v, want both answers and not partial", memory.Titles(out.Results), out.Partial)
	}
	if slates.Sets != 1 {
		t.Errorf("a slate with every provider answered must be cached, sets = %d", slates.Sets)
	}
}

func TestDeadline_WaitsPastItUntilSomethingArrives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bg := memory.NewTracker(t)
		slow := newLateProvider(domain.ProviderSpotify, memory.DeezerTrack("Humble", "Skrillex", 70))
		svc := memory.NewSearchService(t, bg, []ports.SearchProvider{slow}, service.NewCircuitBreaker(clock.Now), service.WithSearchDeadline(time.Millisecond), lateProviderTimeouts)
		done := make(chan *service.SearchOutput, 1)
		go func() { done <- runSearch(t, svc, "humble") }()
		waitFor(t, slow.started, "the provider call to start")

		time.Sleep(time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("the search answered with nothing while a provider was still working")
		default:
		}
		close(slow.release)

		select {
		case out := <-done:
			if len(out.Results) != 1 {
				t.Fatalf("results = %v, want the first answer that arrived", memory.Titles(out.Results))
			}
		case <-time.After(time.Second):
			t.Fatal("search did not answer once the provider did")
		}
	})
}

func TestDeadline_CallerGoneBeforeTheAnswerStopsTheProviders(t *testing.T) {
	bg := memory.NewTracker(t)
	slow := newLateProvider(domain.ProviderSpotify)
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{slow}, service.NewCircuitBreaker(clock.Now), service.WithSearchDeadline(time.Second), lateProviderTimeouts)
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = svc.Execute(ctx, memory.NewUser(), memory.NewQuery(t, "dna"))
	}()
	waitFor(t, slow.started, "the provider call to start")

	cancel()

	waitFor(t, slow.cancelled, "the abandoned search's provider call to stop")
	waitFor(t, returned, "the abandoned search to return")
	bg.Wait()
}

func tripToHalfOpenWindow(t *testing.T, cb *service.CircuitBreaker, provider domain.ProviderName) {
	t.Helper()
	openBreaker(t, cb, provider)
	cb.AdvanceClock(breakerOpenFor + time.Second)
}

func TestFanOut_CanceledHalfOpenProbeDoesNotBlackholeProvider(t *testing.T) {
	bg := memory.NewTracker(t)
	cb := service.NewCircuitBreaker(clock.Now)
	tripToHalfOpenWindow(t, cb, domain.ProviderDeezer)

	p := &memory.FakeProvider{
		Provider: domain.ProviderDeezer,
		Delay:    time.Minute,
		Results:  []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := memory.NewSearchService(t, bg, []ports.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		inspect(t, svc, ctx)
	}()

	timingtest.Until(t, 2*time.Second, "the half-open probe to be admitted", func() bool {
		return cb.Snapshot(domain.ProviderDeezer).Probing
	})
	cancel()
	<-done

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Fatal("provider blackholed: AllowRequest = false after the half-open probe was canceled, want a new probe admitted")
	}
	if got := cb.GetStatus(domain.ProviderDeezer); got != domain.ProviderStatusOK {
		t.Errorf("status after canceled probe = %v, want ok (half-open, not re-opened)", got)
	}
}
