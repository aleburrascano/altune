package search_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type slowTimeoutProvider struct {
	discovery.FakeProvider
	timeout time.Duration
}

func (p *slowTimeoutProvider) SearchTimeout() time.Duration { return p.timeout }

func TestFanOut_PerProviderTimeoutOverride(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	slow := &slowTimeoutProvider{
		FakeProvider: discovery.FakeProvider{
			Provider: discovery.ProviderITunes, Delay: 80 * time.Millisecond,
			Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 70)},
		},
		timeout: 20 * time.Millisecond,
	}
	normal := &discovery.FakeProvider{
		Provider: discovery.ProviderDeezer, Delay: 80 * time.Millisecond,
		Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{slow, normal}, discovery.NewCircuitBreaker())

	results, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status != discovery.ProviderStatusTimeout {
		t.Errorf("overriding provider status = %v, want timeout (its own 20ms budget fired)", statuses[0].Status)
	}
	if statuses[1].Status != discovery.ProviderStatusOK {
		t.Errorf("default provider status = %v, want ok (default budget untouched)", statuses[1].Status)
	}
	if len(results) != 1 || results[0].Popularity != 80 {
		t.Errorf("results = %v, want only the surviving provider's track", discovery.Titles(results))
	}
}

func TestFanOut_TimeoutRecordsBreakerFailure(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	slow := &slowTimeoutProvider{
		FakeProvider: discovery.FakeProvider{Provider: discovery.ProviderITunes, Delay: 80 * time.Millisecond},
		timeout:      10 * time.Millisecond,
	}
	cb := discovery.NewCircuitBreaker()
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{slow}, cb)

	inspect(t, svc, context.Background())

	entry := cb.Snapshot(discovery.ProviderITunes)
	if entry.Failures != 1 {
		t.Errorf("breaker entry = %+v, want 1 recorded failure for the timeout", entry)
	}
}

func TestFanOut_OpenBreakerSkipsProviderEntirely(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	skipped := &discovery.CountingProvider{
		Provider: discovery.ProviderITunes,
		Results:  []discovery.SearchResult{discovery.DeezerTrack("Humble", "x", 10)},
	}
	good := &discovery.FakeProvider{
		Provider: discovery.ProviderDeezer,
		Results:  []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	cb := discovery.NewCircuitBreaker()
	openBreaker(t, cb, discovery.ProviderITunes)
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{skipped, good}, cb)

	results, statuses := inspect(t, svc, context.Background())

	if skipped.Calls != 0 {
		t.Errorf("open-breaker provider was called %d times, want 0", skipped.Calls)
	}
	if statuses[0].Status != discovery.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", statuses[0].Status)
	}
	if statuses[1].Status != discovery.ProviderStatusOK {
		t.Errorf("healthy provider status = %v, want ok", statuses[1].Status)
	}
	if len(results) != 1 || results[0].Popularity != 80 {
		t.Errorf("results = %v, want only the healthy provider's track", discovery.Titles(results))
	}
}

func TestFanOut_ManyProvidersDeterministicSlotOrder(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	names := []discovery.ProviderName{
		discovery.ProviderDeezer, discovery.ProviderITunes, discovery.ProviderMusicBrainz,
		discovery.ProviderSoundCloud, discovery.ProviderTheAudioDB, discovery.ProviderSpotify,
		discovery.ProviderAppleMusic, discovery.ProviderYouTube,
	}
	providers := make([]discovery.SearchProvider, len(names))
	for i, n := range names {
		providers[i] = &discovery.FakeProvider{
			Provider: n,
			Delay:    time.Duration(len(names)-i) * 10 * time.Millisecond,
			Results: []discovery.SearchResult{
				discovery.DeezerTrack(fmt.Sprintf("Humble %d", i), n.String(), float64(50+i)),
			},
		}
	}
	svc := discovery.NewSearchService(bg, providers, discovery.NewCircuitBreaker())

	results, statuses := inspect(t, svc, context.Background())

	if len(statuses) != len(names) || len(results) != len(names) {
		t.Fatalf("statuses=%d results=%d, want %d each", len(statuses), len(results), len(names))
	}
	for i, n := range names {
		if statuses[i].Provider != n {
			t.Errorf("statuses[%d].Provider = %v, want %v (fixed order, not completion order)", i, statuses[i].Provider, n)
		}
		if statuses[i].Status != discovery.ProviderStatusOK {
			t.Errorf("statuses[%d].Status = %v, want ok", i, statuses[i].Status)
		}
	}
}

func breakerFailures(cb *discovery.CircuitBreaker, provider discovery.ProviderName) int {
	return cb.Snapshot(provider).Failures
}

func inspect(t *testing.T, svc *discovery.SearchService, ctx context.Context) ([]discovery.SearchResult, []discovery.ProviderSearchResponse) {
	t.Helper()
	return svc.InspectSearchWithStatuses(ctx, discovery.NewQuery(t, "humble"))
}

func openBreaker(t *testing.T, cb *discovery.CircuitBreaker, provider discovery.ProviderName) {
	t.Helper()
	for range breakerTripsAfter {
		if cb.GetStatus(provider) == discovery.ProviderStatusCircuitOpen {
			t.Fatalf("%s breaker opened before %d failures", provider, breakerTripsAfter)
		}
		cb.RecordFailure(context.Background(), provider)
	}
	if cb.GetStatus(provider) != discovery.ProviderStatusCircuitOpen {
		t.Fatalf("%s breaker still closed after %d failures", provider, breakerTripsAfter)
	}
}

var errQueueShed = fmt.Errorf("%w: %w", discovery.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

func TestFanOut_BreakerCountsOnlyHealthFailures(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"rate-limit queue timeout", fmt.Errorf("mb: all kinds failed: %w", errQueueShed), 0},
		{"parse error", errors.New("invalid character '<'"), 0},
		{"404", discovery.StatusErr(404), 0},
		{"503", discovery.StatusErr(503), 1},
		{"429", discovery.StatusErr(429), 1},
		{"transport", discovery.UpstreamDown, 1},
		{"deadline", context.DeadlineExceeded, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := discovery.NewCircuitBreaker()
			svc := discovery.NewSearchService(bg, []discovery.SearchProvider{&discovery.FakeProvider{Provider: discovery.ProviderITunes, Err: tc.err}}, cb)

			_, statuses := inspect(t, svc, context.Background())

			if got := breakerFailures(cb, discovery.ProviderITunes); got != tc.want {
				t.Errorf("recorded failures = %d, want %d", got, tc.want)
			}
			if statuses[0].Status == discovery.ProviderStatusOK {
				t.Errorf("status = ok for a failed search")
			}
		})
	}
}

func TestFanOut_QueueShedReportsTimeout(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{&discovery.FakeProvider{Provider: discovery.ProviderITunes, Err: errQueueShed}}, discovery.NewCircuitBreaker())

	_, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status != discovery.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout", statuses[0].Status)
	}
}

func TestFanOut_QueueShedProbeReleasesSlot(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	cb := discovery.NewCircuitBreaker()
	tripToHalfOpenWindow(t, cb, discovery.ProviderITunes)
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{&discovery.FakeProvider{Provider: discovery.ProviderITunes, Err: errQueueShed}}, cb)

	_, statuses := inspect(t, svc, context.Background())

	if statuses[0].Status == discovery.ProviderStatusCircuitOpen {
		t.Fatalf("setup: probe not admitted")
	}
	if !cb.AllowRequest(context.Background(), discovery.ProviderITunes) {
		t.Errorf("next request refused: the shed probe held its slot or re-opened the circuit")
	}
}

type ignoresCtxProvider struct{ discovery.FakeProvider }

func (p *ignoresCtxProvider) SearchTimeout() time.Duration { return 5 * time.Millisecond }

func (p *ignoresCtxProvider) Search(ctx context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	<-ctx.Done()
	return nil, errors.New("signal: killed")
}

func TestFanOut_BudgetCutOffUnclassifiedErrorStillCounts(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	cb := discovery.NewCircuitBreaker()
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{&ignoresCtxProvider{discovery.FakeProvider{Provider: discovery.ProviderITunes}}}, cb)

	inspect(t, svc, context.Background())

	if got := breakerFailures(cb, discovery.ProviderITunes); got != 1 {
		t.Errorf("recorded failures = %d, want 1 for a search cut off by its own budget", got)
	}
}

type partialKindsProvider struct {
	discovery.CountingProvider
}

func (p *partialKindsProvider) Search(ctx context.Context, q string, kinds map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	res, _ := p.CountingProvider.Search(ctx, q, kinds)
	return res, &discovery.PartialKindsError{Failed: []discovery.ResultKind{discovery.ResultKindAlbum}, Err: context.DeadlineExceeded}
}

func TestFanOut_PartialKindsKeepResultsButAreNeverCached(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	p := &partialKindsProvider{discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}}
	breaker := discovery.NewCircuitBreaker()
	slates := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{p}, breaker,
		discovery.WithResultCache(slates), discovery.WithProviderResultCache(newFakeProviderCache()))

	out := runSearch(t, svc, "humble")
	runSearch(t, svc, "humble")

	if len(out.Results) != 1 {
		t.Fatalf("results = %v, want the kinds that answered kept", discovery.Titles(out.Results))
	}
	if !out.Partial {
		t.Error("a missing kind must mark the search partial")
	}
	if got := out.ProviderStatuses[0].Status; got != discovery.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (the album search timed out)", got)
	}
	if slates.Sets != 0 || p.Calls != 2 {
		t.Errorf("a partial answer must not be cached: slate sets=%d provider calls=%d", slates.Sets, p.Calls)
	}
	if n := breakerFailures(breaker, discovery.ProviderDeezer); n != 0 {
		t.Errorf("breaker failures = %d, want 0 (the provider answered)", n)
	}
}

type lateProvider struct {
	name      discovery.ProviderName
	results   []discovery.SearchResult
	release   chan struct{}
	started   chan struct{}
	cancelled chan struct{}
	calls     atomic.Int32
}

func newLateProvider(name discovery.ProviderName, results ...discovery.SearchResult) *lateProvider {
	return &lateProvider{
		name:      name,
		results:   results,
		release:   make(chan struct{}),
		started:   make(chan struct{}, 8),
		cancelled: make(chan struct{}, 8),
	}
}

func (p *lateProvider) Name() discovery.ProviderName { return p.name }

func (p *lateProvider) SupportedKinds() map[discovery.ResultKind]bool { return discovery.AllKinds() }

func (p *lateProvider) SearchTimeout() time.Duration { return 5 * time.Second }

func (p *lateProvider) Search(ctx context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
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

func statusOf(out *discovery.SearchOutput, provider discovery.ProviderName) discovery.ProviderSearchResponse {
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	return discovery.ProviderSearchResponse{}
}

func TestDeadline_AnswersWithoutTheSlowProvider(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	fast := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(discovery.ProviderSpotify, discovery.DeezerTrack("Humble", "Skrillex", 70))
	slates := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{fast, slow}, discovery.NewCircuitBreaker(),
		discovery.WithSearchDeadline(20*time.Millisecond), discovery.WithResultCache(slates))
	t.Cleanup(func() { close(slow.release); bg.Wait() })

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %v, want only the fast provider's answer", discovery.Titles(out.Results))
	}
	if st := statusOf(out, discovery.ProviderSpotify).Status; st != discovery.ProviderStatusPending || st.String() != "pending" {
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
	bg := discoverytest.NewTracker(t)
	fast := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	slow := newLateProvider(discovery.ProviderSpotify, discovery.DeezerTrack("Humble", "Skrillex", 70))
	slates := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{fast, slow}, discovery.NewCircuitBreaker(),
		discovery.WithSearchDeadline(20*time.Millisecond), discovery.WithResultCache(slates), discovery.WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	close(slow.release)
	bg.Wait()
	out := runSearch(t, svc, "humble")

	if got := slow.calls.Load(); got != 1 {
		t.Fatalf("slow provider calls = %d, want 1 (its late answer was cached and reused)", got)
	}
	if len(out.Results) != 2 || out.Partial {
		t.Fatalf("second search: results=%v partial=%v, want both answers and not partial", discovery.Titles(out.Results), out.Partial)
	}
	if slates.Sets != 1 {
		t.Errorf("a slate with every provider answered must be cached, sets = %d", slates.Sets)
	}
}

func TestDeadline_WaitsPastItUntilSomethingArrives(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	slow := newLateProvider(discovery.ProviderSpotify, discovery.DeezerTrack("Humble", "Skrillex", 70))
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{slow}, discovery.NewCircuitBreaker(), discovery.WithSearchDeadline(time.Millisecond))
	done := make(chan *discovery.SearchOutput, 1)
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
			t.Fatalf("results = %v, want the first answer that arrived", discovery.Titles(out.Results))
		}
	case <-time.After(time.Second):
		t.Fatal("search did not answer once the provider did")
	}
}

func TestDeadline_CallerGoneBeforeTheAnswerStopsTheProviders(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	slow := newLateProvider(discovery.ProviderSpotify)
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{slow}, discovery.NewCircuitBreaker(), discovery.WithSearchDeadline(time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = svc.Execute(ctx, discovery.NewUser(), discovery.NewQuery(t, "dna"), false) }()
	waitFor(t, slow.started, "the provider call to start")

	cancel()

	waitFor(t, slow.cancelled, "the abandoned search's provider call to stop")
	bg.Wait()
}

func tripToHalfOpenWindow(t *testing.T, cb *discovery.CircuitBreaker, provider discovery.ProviderName) {
	t.Helper()
	openBreaker(t, cb, provider)
	cb.AdvanceClock(breakerOpenFor + time.Second)
}

func TestFanOut_CanceledHalfOpenProbeDoesNotBlackholeProvider(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	cb := discovery.NewCircuitBreaker()
	tripToHalfOpenWindow(t, cb, discovery.ProviderDeezer)

	p := &discovery.FakeProvider{
		Provider: discovery.ProviderDeezer,
		Delay:    time.Minute,
		Results:  []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		inspect(t, svc, ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		probing := cb.Snapshot(discovery.ProviderDeezer).Probing
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

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("provider blackholed: AllowRequest = false after the half-open probe was canceled, want a new probe admitted")
	}
	if got := cb.GetStatus(discovery.ProviderDeezer); got != discovery.ProviderStatusOK {
		t.Errorf("status after canceled probe = %v, want ok (half-open, not re-opened)", got)
	}
}
