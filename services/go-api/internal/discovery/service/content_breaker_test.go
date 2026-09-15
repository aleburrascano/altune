package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
)

// upstreamDown is the transport error a dead provider produces.
var upstreamDown = &url.Error{Op: "Get", URL: "https://provider.test", Err: errors.New("connection refused")}

type statusErr int

func (e statusErr) Error() string   { return fmt.Sprintf("http status %d", int(e)) }
func (e statusErr) HTTPStatus() int { return int(e) }

// tripViaSearch opens provider's circuit the way production does: repeated
// failures of the search fan-out.
func tripViaSearch(t *testing.T, cb *CircuitBreaker, provider domain.ProviderName) {
	t.Helper()
	svc := NewService([]ports.SearchProvider{&fakeProvider{name: provider, err: upstreamDown}}, cb)
	for i := 0; i < failureThreshold; i++ {
		svc.fanOut(context.Background(), "humble", nil)
	}
	if cb.GetStatus(provider) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("setup: %s circuit not open after %d search failures", provider, failureThreshold)
	}
}

func breakerTrack(provider domain.ProviderName, id string) domain.SearchResult {
	return trackFrom(provider, id, "Humble", "Kendrick Lamar")
}

// A provider the search path has tripped open must be skipped by every content
// endpoint instead of being called directly.
func TestContentBreaker_SearchTrippedProviderShortCircuitsContent(t *testing.T) {
	cases := []struct {
		name     string
		provider domain.ProviderName
		run      func(cb *CircuitBreaker, calls *atomic.Int32) (*ContentFetchResponse, error)
	}{
		{
			name:     "top tracks",
			provider: domain.ProviderDeezer,
			run: func(cb *CircuitBreaker, calls *atomic.Int32) (*ContentFetchResponse, error) {
				p := &fakeArtistContentProvider{getTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					calls.Add(1)
					return []domain.SearchResult{breakerTrack(domain.ProviderDeezer, "1")}, nil
				}}
				svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p},
					WithContentCircuitBreaker(cb))
				return svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "42", "", 10)
			},
		},
		{
			name:     "albums",
			provider: domain.ProviderDeezer,
			run: func(cb *CircuitBreaker, calls *atomic.Int32) (*ContentFetchResponse, error) {
				p := &fakeArtistContentProvider{getAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					calls.Add(1)
					return []domain.SearchResult{v2Album(domain.ProviderDeezer, "a", "DAMN.")}, nil
				}}
				svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p},
					WithContentCircuitBreaker(cb))
				return svc.GetAlbums(context.Background(), domain.ProviderDeezer, "42", "", 10)
			},
		},
		{
			name:     "identity fan-out (v2) albums",
			provider: domain.ProviderDeezer,
			run: func(cb *CircuitBreaker, calls *atomic.Int32) (*ContentFetchResponse, error) {
				p := &fakeArtistContentProvider{getAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					calls.Add(1)
					return []domain.SearchResult{v2Album(domain.ProviderDeezer, "a", "DAMN.")}, nil
				}}
				svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p},
					WithContentIdentityStore(&fakeIdentityStore{}), WithContentCircuitBreaker(cb))
				return svc.GetAlbums(context.Background(), domain.ProviderDeezer, "42", "", 10)
			},
		},
		{
			name:     "album tracks with deezer search fallback",
			provider: domain.ProviderDeezer,
			run: func(cb *CircuitBreaker, calls *atomic.Int32) (*ContentFetchResponse, error) {
				p := &fakeAlbumContentProvider{getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					calls.Add(1)
					return []domain.SearchResult{breakerTrack(domain.ProviderDeezer, "1")}, nil
				}}
				searcher := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{albumSearchResult("Kendrick Lamar", "9")}}
				svc := NewGetAlbumTracksService(map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: p},
					WithAlbumFallbackSearcher(searcher), WithAlbumCircuitBreaker(cb))
				resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{
					Provider: domain.ProviderDeezer, ExternalID: "7", Title: "DAMN.", Artist: "Kendrick Lamar",
				})
				calls.Add(int32(searcher.calls))
				return resp, err
			},
		},
		{
			name:     "related tracks",
			provider: domain.ProviderSoundCloud,
			run: func(cb *CircuitBreaker, calls *atomic.Int32) (*ContentFetchResponse, error) {
				p := &countingRelatedProvider{calls: calls}
				svc := NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{"soundcloud": p},
					WithRelatedCircuitBreaker(cb))
				return svc.Execute(context.Background(), domain.ProviderSoundCloud, "7", 10)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := NewCircuitBreaker()
			tripViaSearch(t, cb, tc.provider)

			var calls atomic.Int32
			resp, err := tc.run(cb, &calls)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("open-circuit provider %s was called %d times by the content endpoint, want 0", tc.provider, n)
			}
			if len(resp.Items) != 0 {
				t.Errorf("items = %d, want none from a short-circuited provider", len(resp.Items))
			}
		})
	}
}

func TestContentBreaker_SingleProviderOpenCircuitReportsCircuitOpen(t *testing.T) {
	cb := NewCircuitBreaker()
	tripViaSearch(t, cb, domain.ProviderDeezer)
	svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: &fakeArtistContentProvider{},
	}, WithContentCircuitBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "42", "", 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Status != domain.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", resp.Status)
	}
}

// Content-path failures count toward the same breaker, so a provider that dies
// on content calls is also skipped by the search fan-out.
func TestContentBreaker_ContentFailuresTripSearch(t *testing.T) {
	cb := NewCircuitBreaker()
	var contentCalls atomic.Int32
	p := &fakeArtistContentProvider{getTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
		contentCalls.Add(1)
		return nil, upstreamDown
	}}
	content := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p},
		WithContentCircuitBreaker(cb))
	for i := 0; i < failureThreshold+2; i++ {
		if _, err := content.GetTopTracks(context.Background(), domain.ProviderDeezer, "42", "", 10); err != nil {
			t.Fatalf("error = %v", err)
		}
	}
	if n := contentCalls.Load(); n != failureThreshold {
		t.Errorf("content calls = %d, want %d (calls after the circuit opened are skipped)", n, failureThreshold)
	}

	searcher := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{breakerTrack(domain.ProviderDeezer, "1")}}
	_, statuses := NewService([]ports.SearchProvider{searcher}, cb).fanOut(context.Background(), "humble", nil)
	if searcher.calls != 0 || statuses[0].Status != domain.ProviderStatusCircuitOpen {
		t.Errorf("search calls = %d status = %v, want 0 calls and circuit_open after content failures",
			searcher.calls, statuses[0].Status)
	}
}

func TestContentBreaker_OnlyHealthFailuresCount(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantOpen bool
	}{
		{"transport error", upstreamDown, true},
		{"provider timeout", fmt.Errorf("fetch: %w", context.DeadlineExceeded), true},
		{"5xx", fmt.Errorf("musicbrainz status 503: %w", statusErr(503)), true},
		{"429", statusErr(429), true},
		{"404 for an unknown id", statusErr(404), false},
		{"provider data error for a bogus id", errors.New("deezer api error 800 (DataException): no data"), false},
		{"canceled", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := NewCircuitBreaker()
			p := &fakeRelatedProvider{err: tc.err}
			svc := NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{"soundcloud": p},
				WithRelatedCircuitBreaker(cb))
			for i := 0; i < failureThreshold*2; i++ {
				_, _ = svc.Execute(context.Background(), domain.ProviderSoundCloud, "bogus", 10)
			}
			if open := cb.GetStatus(domain.ProviderSoundCloud) == domain.ProviderStatusCircuitOpen; open != tc.wantOpen {
				t.Errorf("circuit open = %v, want %v", open, tc.wantOpen)
			}
		})
	}
}

// A half-open probe made by a content call must resolve the slot on every
// outcome, or the provider stays blackholed for search too.
func TestContentBreaker_HalfOpenProbeOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		cancel    bool
		err       error
		panics    bool
		wantState CircuitState
	}{
		{name: "success closes", wantState: CircuitClosed},
		{name: "failure reopens", err: upstreamDown, wantState: CircuitOpen},
		{name: "caller cancel releases", cancel: true, err: upstreamDown, wantState: CircuitHalfOpen},
		{name: "request-scoped error releases", err: statusErr(404), wantState: CircuitHalfOpen},
		{name: "panic reopens", panics: true, wantState: CircuitOpen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := NewCircuitBreaker()
			tripToHalfOpenWindow(cb, domain.ProviderDeezer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &fakeArtistContentProvider{getAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
				if tc.cancel {
					cancel()
				}
				if tc.panics {
					panic("boom")
				}
				return []domain.SearchResult{v2Album(domain.ProviderDeezer, "a", "DAMN.")}, tc.err
			}}
			svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p},
				WithContentIdentityStore(&fakeIdentityStore{}), WithContentCircuitBreaker(cb))

			_, _ = svc.GetAlbums(ctx, domain.ProviderDeezer, "42", "", 10)

			cb.mu.Lock()
			entry := *cb.getOrCreate(domain.ProviderDeezer)
			cb.mu.Unlock()
			if entry.state != tc.wantState {
				t.Errorf("state = %v, want %v", entry.state, tc.wantState)
			}
			if entry.state == CircuitHalfOpen && !cb.AllowRequest(domain.ProviderDeezer) {
				t.Error("probe slot still held: next request not admitted")
			}
		})
	}
}

type countingRelatedProvider struct {
	calls *atomic.Int32
}

func (p *countingRelatedProvider) GetRelatedTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	p.calls.Add(1)
	return []domain.SearchResult{breakerTrack(domain.ProviderSoundCloud, "1")}, nil
}
