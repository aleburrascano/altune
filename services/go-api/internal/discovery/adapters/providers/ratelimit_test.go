package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestMinIntervalLimiterCancelledCtxReturnsPromptly(t *testing.T) {
	l := newMinIntervalLimiter(time.Hour)

	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := l.wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("wait blocked for %v; should return promptly on cancellation", elapsed)
	}
}

func TestMinIntervalLimiterShortDeadlineReturnsBeforeInterval(t *testing.T) {
	l := newMinIntervalLimiter(time.Hour)

	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := l.wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("wait blocked for %v; should return before the full interval", elapsed)
	}
}

func TestMinIntervalLimiterEnforcesInterval(t *testing.T) {
	l := newMinIntervalLimiter(50 * time.Millisecond)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatalf("wait %d: unexpected error %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Fatalf("three waits took %v; expected at least two full intervals", elapsed)
	}
}

func TestMinIntervalLimiterShedsWithoutReservingSlot(t *testing.T) {
	l := newMinIntervalLimiter(time.Hour)
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}
	l.mu.Lock()
	before := l.lastReq
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := l.wait(ctx)

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a queue timeout that is also DeadlineExceeded, got %v", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lastReq.Equal(before) {
		t.Errorf("shed caller reserved a slot: lastReq moved from %v to %v", before, l.lastReq)
	}
}

// A caller whose deadline had already passed before it reached the queue spent
// its budget elsewhere; that is a plain timeout, not a queue timeout.
func TestMinIntervalLimiterExpiredOnArrivalIsNotQueueTimeout(t *testing.T) {
	l := newMinIntervalLimiter(time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := l.wait(ctx)

	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		t.Fatalf("want plain DeadlineExceeded, got %v", err)
	}
}

// shortBudgetMB is a real MusicBrainz adapter, real limiter and all, with a
// per-provider search budget short enough to exercise queueing in a test.
type shortBudgetMB struct {
	*MusicBrainzAdapter
	budget time.Duration
}

func (a shortBudgetMB) SearchTimeout() time.Duration { return a.budget }

func newShortBudgetMB(serverURL string, interval, budget time.Duration) shortBudgetMB {
	mb := NewMusicBrainzAdapter(newTestClient(serverURL), "altune-test/1.0")
	mb.limiter = newMinIntervalLimiter(interval)
	return shortBudgetMB{MusicBrainzAdapter: mb, budget: budget}
}

var trackOnly = map[domain.ResultKind]bool{domain.ResultKindTrack: true}

func healthyMBServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recordings":[{"id":"rec-1","title":"Humble","artist-credit":[{"name":"Kendrick Lamar"}]}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func inspect(t *testing.T, svc *service.Service) {
	inspectKinds(t, svc, trackOnly)
}

func inspectKinds(t *testing.T, svc *service.Service, kinds map[domain.ResultKind]bool) {
	q, err := domain.NewSearchQuery("humble", kinds, 10)
	if err != nil {
		t.Errorf("query: %v", err)
		return
	}
	_, _ = svc.InspectSearchWithStatuses(context.Background(), q)
}

func searchConcurrently(t *testing.T, svc *service.Service, n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inspect(t, svc)
		}()
	}
	wg.Wait()
}

// A healthy provider answers one search, which takes the limiter's slot; a
// burst of concurrent searches right behind it finds the next slot beyond its
// budget. None of the burst ever reaches the provider, so none of it may count
// against the circuit. (No success follows the burst, so a miscounted failure
// cannot be masked by a later success resetting the count.)
func TestRateLimitQueueTimeoutsDoNotOpenCircuit(t *testing.T) {
	srv := healthyMBServer(t)
	mb := newShortBudgetMB(srv.URL, 200*time.Millisecond, 50*time.Millisecond)
	cb := service.NewCircuitBreaker()
	svc := service.NewService([]ports.SearchProvider{mb}, cb)
	inspect(t, svc)

	searchConcurrently(t, svc, 10)

	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusOK {
		t.Fatalf("circuit status = %v after a queue-only overload of a healthy provider, want ok", got)
	}
}

// The limiter sheds a call whose slot lands past the caller's deadline at
// once, instead of holding it in the queue until the deadline fires.
func TestRateLimitQueueTimeoutIsShedEarly(t *testing.T) {
	srv := healthyMBServer(t)
	mb := newShortBudgetMB(srv.URL, time.Hour, time.Second)
	if _, err := mb.Search(context.Background(), "humble", trackOnly); err != nil {
		t.Fatalf("first search: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := mb.Search(ctx, "humble", trackOnly)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("queued search took %v, want it shed immediately", elapsed)
	}
	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		t.Errorf("err = %v, want ports.ErrProviderRateLimitQueueTimeout", err)
	}
}

// A provider whose upstream genuinely hangs past the budget still trips. All
// kinds are requested: the first kind's hung request spends the whole budget,
// and the later kinds reaching the limiter already expired must read as that
// timeout, not as a queue shed.
func TestRealUpstreamTimeoutStillOpensCircuit(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	mb := newShortBudgetMB(srv.URL, time.Millisecond, 30*time.Millisecond)
	cb := service.NewCircuitBreaker()
	svc := service.NewService([]ports.SearchProvider{mb}, cb)

	allKinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack: true, domain.ResultKindAlbum: true, domain.ResultKindArtist: true,
	}
	for i := 0; i < 5; i++ {
		inspectKinds(t, svc, allKinds)
	}

	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusCircuitOpen {
		t.Fatalf("circuit status = %v after 5 upstream timeouts, want circuit_open", got)
	}
}

// A provider answering 5xx still trips.
func TestUpstream5xxStillOpensCircuit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	mb := newShortBudgetMB(srv.URL, time.Millisecond, time.Second)
	cb := service.NewCircuitBreaker()
	svc := service.NewService([]ports.SearchProvider{mb}, cb)

	for i := 0; i < 5; i++ {
		inspect(t, svc)
	}

	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusCircuitOpen {
		t.Fatalf("circuit status = %v after 5 upstream 503s, want circuit_open", got)
	}
}

// With a search budget far longer than the queue can hold, a burst of
// concurrent searches is shed by queue depth rather than by deadline. Those
// sheds come back promptly as timeouts and never count against the circuit.
func TestRateLimitQueueFullShedsDoNotOpenCircuit(t *testing.T) {
	srv := healthyMBServer(t)
	mb := newShortBudgetMB(srv.URL, 100*time.Millisecond, time.Minute)
	cb := service.NewCircuitBreaker()
	svc := service.NewService([]ports.SearchProvider{mb}, cb)
	q, err := domain.NewSearchQuery("humble", trackOnly, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	const searches = 12
	statuses := make(chan domain.ProviderStatus, searches)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < searches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, st := svc.InspectSearchWithStatuses(context.Background(), q)
			for _, s := range st {
				statuses <- s.Status
			}
		}()
	}
	wg.Wait()
	close(statuses)

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("burst took %v, want excess searches shed rather than queued", elapsed)
	}
	counts := map[domain.ProviderStatus]int{}
	for s := range statuses {
		counts[s]++
	}
	if want := 1 + providerQueueDepth; counts[domain.ProviderStatusOK] != want {
		t.Errorf("ok searches = %d, want %d (1 immediate + queue depth); counts %v",
			counts[domain.ProviderStatusOK], want, counts)
	}
	if want := searches - 1 - providerQueueDepth; counts[domain.ProviderStatusTimeout] != want {
		t.Errorf("timeout searches = %d, want %d shed; counts %v",
			counts[domain.ProviderStatusTimeout], want, counts)
	}
	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusOK {
		t.Fatalf("circuit status = %v after a queue-full overload of a healthy provider, want ok", got)
	}
}

// burstResult is what one caller in a concurrent burst saw from the limiter.
type burstResult struct {
	err     error
	elapsed time.Duration
}

// fireBurst sends n concurrent callers at wait, each with a deadline far past
// any sane queue budget, and returns what the callers that finished within
// window saw. The rest are cancelled and drained before it returns.
func fireBurst(t *testing.T, wait func(context.Context) error, n int, window time.Duration) []burstResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	results := make(chan burstResult, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := wait(ctx)
			results <- burstResult{err: err, elapsed: time.Since(start)}
		}()
	}

	timer := time.NewTimer(window)
	defer timer.Stop()
	var done []burstResult
collect:
	for len(done) < n {
		select {
		case r := <-results:
			done = append(done, r)
		case <-timer.C:
			break collect
		}
	}
	cancel()
	wg.Wait()
	return done
}

// A burst of concurrent callers into a production-configured provider limiter
// admits its burst at once, parks at most providerQueueDepth callers for later
// slots, and sheds every other caller promptly with the queue-timeout sentinel
// the circuit breaker ignores, instead of parking it for as long as its
// deadline allows.
func TestProviderLimiterBurstShedsBeyondQueueDepth(t *testing.T) {
	const callers = 20
	cases := []struct {
		name  string
		wait  func(context.Context) error
		burst int
	}{
		{"musicbrainz", NewMusicBrainzAdapter(http.DefaultClient, "ua").limiter.wait, 1},
		{"discogs", NewDiscogsAdapter(http.DefaultClient, "tok", "ua").limiter.wait, 1},
		{"itunes", NewITunesAdapter(http.DefaultClient).limiter.wait, itunesBurst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fireBurst(t, tc.wait, callers, 300*time.Millisecond)

			admitted, shed := 0, 0
			for _, r := range got {
				switch {
				case r.err == nil:
					admitted++
				case errors.Is(r.err, ports.ErrProviderRateLimitQueueTimeout):
					shed++
				default:
					t.Errorf("caller got %v, want nil or a queue-timeout shed", r.err)
				}
			}
			if admitted != tc.burst {
				t.Errorf("admitted at once = %d, want the burst of %d", admitted, tc.burst)
			}
			if want := callers - tc.burst - providerQueueDepth; shed != want {
				t.Errorf("shed within 300ms = %d, want %d (callers %d - burst %d - queue depth %d)",
					shed, want, callers, tc.burst, providerQueueDepth)
			}
		})
	}
}

// The depth bound does not loosen politeness: callers admitted out of a burst
// still get slots at least one interval apart.
func TestRateLimiterAdmittedCallersStaySpaced(t *testing.T) {
	const interval = 60 * time.Millisecond
	l := newRateLimiter(interval, 1, 3)

	got := fireBurst(t, l.wait, 10, 2*time.Second)

	var admits []time.Duration
	for _, r := range got {
		if r.err == nil {
			admits = append(admits, r.elapsed)
		}
	}
	if len(admits) != 4 {
		t.Fatalf("admitted %d callers, want 1 immediate + 3 queued", len(admits))
	}
	slices.Sort(admits)
	for i := 1; i < len(admits); i++ {
		if gap := admits[i] - admits[i-1]; gap < interval-10*time.Millisecond {
			t.Errorf("admits %d and %d only %v apart, want >= %v", i-1, i, gap, interval)
		}
	}
}

// The queue depth frees up as slots are consumed: once the queue drains, a new
// caller is admitted rather than shed.
func TestRateLimiterQueueDrainsAndReadmits(t *testing.T) {
	const interval = 30 * time.Millisecond
	l := newRateLimiter(interval, 1, 1)
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("second (queued): %v", err)
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("third after the queue drained: %v", err)
	}
}

// A caller with no deadline at all is still shed once the queue is full.
func TestRateLimiterShedsNoDeadlineCallerWhenFull(t *testing.T) {
	l := newRateLimiter(time.Hour, 1, 2)
	_ = l.wait(context.Background())
	l.mu.Lock()
	l.lastReq = l.lastReq.Add(2 * time.Hour) // two callers already queued
	before := l.lastReq
	l.mu.Unlock()

	start := time.Now()
	err := l.wait(context.Background())

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a queue-timeout shed that is also DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("shed took %v, want immediate", elapsed)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lastReq.Equal(before) {
		t.Errorf("shed caller reserved a slot: lastReq moved from %v to %v", before, l.lastReq)
	}
}

// iTunes keeps its burst: the first itunesBurst calls go straight through.
func TestITunesLimiterAllowsBurst(t *testing.T) {
	a := NewITunesAdapter(http.DefaultClient)
	start := time.Now()
	for i := 0; i < itunesBurst; i++ {
		if err := a.limiter.wait(context.Background()); err != nil {
			t.Fatalf("burst call %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("burst of %d took %v, want immediate", itunesBurst, elapsed)
	}
}
