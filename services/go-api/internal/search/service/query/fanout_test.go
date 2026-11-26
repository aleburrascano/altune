package query_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type slowTimeoutProvider struct {
	search.FakeProvider
	timeout time.Duration
}

func (p *slowTimeoutProvider) SearchTimeout() time.Duration { return p.timeout }

func TestFanOut_PerProviderTimeoutOverride(t *testing.T) {
	bg := searchtest.NewTracker(t)
	slow := &slowTimeoutProvider{
		FakeProvider: search.FakeProvider{
			Provider: search.ProviderITunes, Delay: 80 * time.Millisecond,
			Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 70)},
		},
		timeout: 20 * time.Millisecond,
	}
	normal := &search.FakeProvider{
		Provider: search.ProviderDeezer, Delay: 80 * time.Millisecond,
		Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := search.NewSearchService(bg, []search.SearchProvider{slow, normal}, search.NewCircuitBreaker())

	results, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status != search.ProviderStatusTimeout {
		t.Errorf("overriding provider status = %v, want timeout (its own 20ms budget fired)", statuses[0].Status)
	}
	if statuses[1].Status != search.ProviderStatusOK {
		t.Errorf("default provider status = %v, want ok (default budget untouched)", statuses[1].Status)
	}
	if len(results) != 1 || results[0].Popularity != 80 {
		t.Errorf("results = %v, want only the surviving provider's track", search.Titles(results))
	}
}

func TestFanOut_TimeoutRecordsBreakerFailure(t *testing.T) {
	bg := searchtest.NewTracker(t)
	slow := &slowTimeoutProvider{
		FakeProvider: search.FakeProvider{Provider: search.ProviderITunes, Delay: 80 * time.Millisecond},
		timeout:      10 * time.Millisecond,
	}
	cb := search.NewCircuitBreaker()
	svc := search.NewSearchService(bg, []search.SearchProvider{slow}, cb)

	inspect(t, svc, context.Background())

	entry := cb.Snapshot(search.ProviderITunes)
	if entry.Failures != 1 {
		t.Errorf("breaker entry = %+v, want 1 recorded failure for the timeout", entry)
	}
}

func TestFanOut_OpenBreakerSkipsProviderEntirely(t *testing.T) {
	bg := searchtest.NewTracker(t)
	skipped := &search.CountingProvider{
		Provider: search.ProviderITunes,
		Results:  []search.SearchResult{search.DeezerTrack("Humble", "x", 10)},
	}
	good := &search.FakeProvider{
		Provider: search.ProviderDeezer,
		Results:  []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	cb := search.NewCircuitBreaker()
	openBreaker(t, cb, search.ProviderITunes)
	svc := search.NewSearchService(bg, []search.SearchProvider{skipped, good}, cb)

	results, statuses := inspect(t, svc, context.Background())

	if skipped.Calls != 0 {
		t.Errorf("open-breaker provider was called %d times, want 0", skipped.Calls)
	}
	if statuses[0].Status != search.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", statuses[0].Status)
	}
	if statuses[1].Status != search.ProviderStatusOK {
		t.Errorf("healthy provider status = %v, want ok", statuses[1].Status)
	}
	if len(results) != 1 || results[0].Popularity != 80 {
		t.Errorf("results = %v, want only the healthy provider's track", search.Titles(results))
	}
}

func TestFanOut_ManyProvidersDeterministicSlotOrder(t *testing.T) {
	bg := searchtest.NewTracker(t)
	names := []search.ProviderName{
		search.ProviderDeezer, search.ProviderITunes, search.ProviderMusicBrainz,
		search.ProviderSoundCloud, search.ProviderTheAudioDB, search.ProviderSpotify,
		search.ProviderAppleMusic, search.ProviderYouTube,
	}
	providers := make([]search.SearchProvider, len(names))
	for i, n := range names {
		providers[i] = &search.FakeProvider{
			Provider: n,
			Delay:    time.Duration(len(names)-i) * 10 * time.Millisecond,
			Results: []search.SearchResult{
				search.DeezerTrack(fmt.Sprintf("Humble %d", i), n.String(), float64(50+i)),
			},
		}
	}
	svc := search.NewSearchService(bg, providers, search.NewCircuitBreaker())

	results, statuses := inspect(t, svc, context.Background())

	if len(statuses) != len(names) || len(results) != len(names) {
		t.Fatalf("statuses=%d results=%d, want %d each", len(statuses), len(results), len(names))
	}
	for i, n := range names {
		if statuses[i].Provider != n {
			t.Errorf("statuses[%d].Provider = %v, want %v (fixed order, not completion order)", i, statuses[i].Provider, n)
		}
		if statuses[i].Status != search.ProviderStatusOK {
			t.Errorf("statuses[%d].Status = %v, want ok", i, statuses[i].Status)
		}
	}
}

func breakerFailures(cb *search.CircuitBreaker, provider search.ProviderName) int {
	return cb.Snapshot(provider).Failures
}

func inspect(t *testing.T, svc *search.SearchService, ctx context.Context) ([]search.SearchResult, []search.ProviderSearchResponse) {
	t.Helper()
	return svc.InspectSearchWithStatuses(ctx, search.NewQuery(t, "humble"))
}

func openBreaker(t *testing.T, cb *search.CircuitBreaker, provider search.ProviderName) {
	t.Helper()
	for range breakerTripsAfter {
		if cb.GetStatus(provider) == search.ProviderStatusCircuitOpen {
			t.Fatalf("%s breaker opened before %d failures", provider, breakerTripsAfter)
		}
		cb.RecordFailure(context.Background(), provider, search.CircuitAdmission{})
	}
	if cb.GetStatus(provider) != search.ProviderStatusCircuitOpen {
		t.Fatalf("%s breaker still closed after %d failures", provider, breakerTripsAfter)
	}
}

var errQueueShed = fmt.Errorf("%w: %w", search.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

func TestFanOut_BreakerCountsOnlyHealthFailures(t *testing.T) {
	bg := searchtest.NewTracker(t)
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"rate-limit queue timeout", fmt.Errorf("mb: all kinds failed: %w", errQueueShed), 0},
		{"parse error", errors.New("invalid character '<'"), 0},
		{"404", search.UpstreamStatusError(404), 0},
		{"503", search.UpstreamStatusError(503), 1},
		{"429", search.UpstreamStatusError(429), 1},
		{"transport", search.ErrUpstreamDown, 1},
		{"deadline", context.DeadlineExceeded, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := search.NewCircuitBreaker()
			svc := search.NewSearchService(bg, []search.SearchProvider{&search.FakeProvider{Provider: search.ProviderITunes, Err: tc.err}}, cb)

			_, statuses := inspect(t, svc, context.Background())

			if got := breakerFailures(cb, search.ProviderITunes); got != tc.want {
				t.Errorf("recorded failures = %d, want %d", got, tc.want)
			}
			if statuses[0].Status == search.ProviderStatusOK {
				t.Errorf("status = ok for a failed search")
			}
		})
	}
}

func TestFanOut_QueueShedReportsTimeout(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := search.NewSearchService(bg, []search.SearchProvider{&search.FakeProvider{Provider: search.ProviderITunes, Err: errQueueShed}}, search.NewCircuitBreaker())

	_, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status != search.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout", statuses[0].Status)
	}
}

func TestFanOut_QueueShedProbeReleasesSlot(t *testing.T) {
	bg := searchtest.NewTracker(t)
	cb := search.NewCircuitBreaker()
	tripToHalfOpenWindow(t, cb, search.ProviderITunes)
	svc := search.NewSearchService(bg, []search.SearchProvider{&search.FakeProvider{Provider: search.ProviderITunes, Err: errQueueShed}}, cb)

	_, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status == search.ProviderStatusCircuitOpen {
		t.Fatalf("setup: probe not admitted")
	}
	if _, ok := cb.AllowRequest(context.Background(), search.ProviderITunes); !ok {
		t.Errorf("next request refused: the shed probe held its slot or re-opened the circuit")
	}
}

type ignoresCtxProvider struct{ search.FakeProvider }

func (p *ignoresCtxProvider) SearchTimeout() time.Duration { return 5 * time.Millisecond }

func (p *ignoresCtxProvider) Search(ctx context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	<-ctx.Done()
	return nil, errors.New("signal: killed")
}

func TestFanOut_BudgetCutOffUnclassifiedErrorStillCounts(t *testing.T) {
	bg := searchtest.NewTracker(t)
	cb := search.NewCircuitBreaker()
	svc := search.NewSearchService(bg, []search.SearchProvider{&ignoresCtxProvider{search.FakeProvider{Provider: search.ProviderITunes}}}, cb)

	inspect(t, svc, context.Background())

	if got := breakerFailures(cb, search.ProviderITunes); got != 1 {
		t.Errorf("recorded failures = %d, want 1 for a search cut off by its own budget", got)
	}
}

type partialKindsProvider struct {
	search.CountingProvider
}

func (p *partialKindsProvider) Search(ctx context.Context, q string, kinds map[search.ResultKind]bool) ([]search.SearchResult, error) {
	res, _ := p.CountingProvider.Search(ctx, q, kinds)
	return res, &search.PartialKindsError{Failed: []search.ResultKind{search.ResultKindAlbum}, Err: context.DeadlineExceeded}
}

func TestFanOut_PartialKindsKeepResultsButAreNeverCached(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &partialKindsProvider{search.CountingProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}}
	breaker := search.NewCircuitBreaker()
	slates := search.NewFakeResultCache()
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, breaker,
		search.WithResultCache(slates), search.WithProviderResultCache(newFakeProviderCache()))

	out := runSearch(t, svc, "humble")
	runSearch(t, svc, "humble")

	if len(out.Results) != 1 {
		t.Fatalf("results = %v, want the kinds that answered kept", search.Titles(out.Results))
	}
	if !out.Partial {
		t.Error("a missing kind must mark the search partial")
	}
	if got := out.ProviderStatuses[0].Status; got != search.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (the album search timed out)", got)
	}
	if slates.Sets != 0 || p.Calls != 2 {
		t.Errorf("a partial answer must not be cached: slate sets=%d provider calls=%d", slates.Sets, p.Calls)
	}
	if n := breakerFailures(breaker, search.ProviderDeezer); n != 0 {
		t.Errorf("breaker failures = %d, want 0 (the provider answered)", n)
	}
}

type lateProvider struct {
	name      search.ProviderName
	results   []search.SearchResult
	release   chan struct{}
	started   chan struct{}
	cancelled chan struct{}
	calls     atomic.Int32
}

func newLateProvider(name search.ProviderName, results ...search.SearchResult) *lateProvider {
	return &lateProvider{
		name:      name,
		results:   results,
		release:   make(chan struct{}),
		started:   make(chan struct{}, 8),
		cancelled: make(chan struct{}, 8),
	}
}

func (p *lateProvider) Name() search.ProviderName { return p.name }

func (p *lateProvider) SupportedKinds() map[search.ResultKind]bool { return search.AllKinds() }

func (p *lateProvider) SearchTimeout() time.Duration { return 5 * time.Second }

func (p *lateProvider) Search(ctx context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
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

func statusOf(out *search.SearchOutput, provider search.ProviderName) search.ProviderSearchResponse {
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	return search.ProviderSearchResponse{}
}

func TestDeadline_AnswersWithoutTheSlowProvider(t *testing.T) {
	bg := searchtest.NewTracker(t)
	fast := &search.CountingProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(search.ProviderSpotify, search.DeezerTrack("Humble", "Skrillex", 70))
	slates := search.NewFakeResultCache()
	svc := search.NewSearchService(bg, []search.SearchProvider{fast, slow}, search.NewCircuitBreaker(),
		search.WithSearchDeadline(20*time.Millisecond), search.WithResultCache(slates))
	t.Cleanup(func() { close(slow.release); bg.Wait() })

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %v, want only the fast provider's answer", search.Titles(out.Results))
	}
	if st := statusOf(out, search.ProviderSpotify).Status; st != search.ProviderStatusPending || st.String() != "pending" {
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
	bg := searchtest.NewTracker(t)
	fast := &search.CountingProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(search.ProviderSpotify, search.DeezerTrack("Humble", "Skrillex", 70))
	slates := search.NewFakeResultCache()
	svc := search.NewSearchService(bg, []search.SearchProvider{fast, slow}, search.NewCircuitBreaker(),
		search.WithSearchDeadline(20*time.Millisecond), search.WithResultCache(slates), search.WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	close(slow.release)
	bg.Wait()
	out := runSearch(t, svc, "humble")

	if got := slow.calls.Load(); got != 1 {
		t.Fatalf("slow provider calls = %d, want 1 (its late answer was cached and reused)", got)
	}
	if len(out.Results) != 2 || out.Partial {
		t.Fatalf("second search: results=%v partial=%v, want both answers and not partial", search.Titles(out.Results), out.Partial)
	}
	if slates.Sets != 1 {
		t.Errorf("a slate with every provider answered must be cached, sets = %d", slates.Sets)
	}
}

func TestDeadline_WaitsPastItUntilSomethingArrives(t *testing.T) {
	bg := searchtest.NewTracker(t)
	slow := newLateProvider(search.ProviderSpotify, search.DeezerTrack("Humble", "Skrillex", 70))
	svc := search.NewSearchService(bg, []search.SearchProvider{slow}, search.NewCircuitBreaker(), search.WithSearchDeadline(time.Millisecond))
	done := make(chan *search.SearchOutput, 1)
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
			t.Fatalf("results = %v, want the first answer that arrived", search.Titles(out.Results))
		}
	case <-time.After(time.Second):
		t.Fatal("search did not answer once the provider did")
	}
}

func TestDeadline_CallerGoneBeforeTheAnswerStopsTheProviders(t *testing.T) {
	bg := searchtest.NewTracker(t)
	slow := newLateProvider(search.ProviderSpotify)
	svc := search.NewSearchService(bg, []search.SearchProvider{slow}, search.NewCircuitBreaker(), search.WithSearchDeadline(time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = svc.Execute(ctx, search.NewUser(), search.NewQuery(t, "dna")) }()
	waitFor(t, slow.started, "the provider call to start")

	cancel()

	waitFor(t, slow.cancelled, "the abandoned search's provider call to stop")
	bg.Wait()
}

func tripToHalfOpenWindow(t *testing.T, cb *search.CircuitBreaker, provider search.ProviderName) {
	t.Helper()
	openBreaker(t, cb, provider)
	cb.AdvanceClock(breakerOpenFor + time.Second)
}

func TestFanOut_CanceledHalfOpenProbeDoesNotBlackholeProvider(t *testing.T) {
	bg := searchtest.NewTracker(t)
	cb := search.NewCircuitBreaker()
	tripToHalfOpenWindow(t, cb, search.ProviderDeezer)

	p := &search.FakeProvider{
		Provider: search.ProviderDeezer,
		Delay:    time.Minute,
		Results:  []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		inspect(t, svc, ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		probing := cb.Snapshot(search.ProviderDeezer).Probing
		if probing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("half-open probe was never admitted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if _, ok := cb.AllowRequest(context.Background(), search.ProviderDeezer); !ok {
		t.Fatal("provider blackholed: AllowRequest = false after the half-open probe was canceled, want a new probe admitted")
	}
	if got := cb.GetStatus(search.ProviderDeezer); got != search.ProviderStatusOK {
		t.Errorf("status after canceled probe = %v, want ok (half-open, not re-opened)", got)
	}
}
