package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

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
