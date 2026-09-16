package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
)

type GetArtistContentService struct {
	providers            map[domain.ProviderName]ports.ArtistContentProvider
	consensus            *ConsensusService
	identityStore        ports.IdentityStore
	mbAnchor             ports.MBDiscographyAnchor
	breaker              *CircuitBreaker
	discographyTelemetry *DiscographyTelemetry
}

func NewGetArtistContentService(
	providers map[domain.ProviderName]ports.ArtistContentProvider,
	opts ...ArtistContentOption,
) *GetArtistContentService {
	s := &GetArtistContentService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type ArtistContentOption func(*GetArtistContentService)

func WithConsensusService(c *ConsensusService) ArtistContentOption {
	return func(s *GetArtistContentService) { s.consensus = c }
}

func WithContentIdentityStore(store ports.IdentityStore) ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityStore = store }
}

// WithContentCircuitBreaker gates every provider call the service makes through
// cb. Pass the search fan-out's breaker so a provider tripped open by either
// path is short-circuited on both. Without it, calls are ungated.
func WithContentCircuitBreaker(cb *CircuitBreaker) ArtistContentOption {
	return func(s *GetArtistContentService) { s.breaker = cb }
}

func WithMBAnchor(anchor ports.MBDiscographyAnchor) ArtistContentOption {
	return func(s *GetArtistContentService) { s.mbAnchor = anchor }
}

// WithContentEventStore injects the event store the v2 discography path emits
// its server-only discography_observed structural-quality signal through. Absent
// it, the service records nothing (the emit is a no-op), so the signal is purely
// additive and never on the request's critical path.
func WithContentEventStore(eventStore ports.EventStore) ArtistContentOption {
	return func(s *GetArtistContentService) {
		if eventStore != nil {
			s.discographyTelemetry = newDiscographyTelemetry(eventStore)
		}
	}
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityStore != nil {
		identity, _ := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		if tracks, partial := s.v2TopTracks(ctx, identity); len(tracks) > 0 {
			resp := okContentResponse(providerName, tracks, limit)
			resp.Partial = partial
			return resp, nil
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return unservedContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, s.breaker, providerName, externalID, "artist_top_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistTopTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return okContentResponse(providerName, results, limit), nil
}

type identityContentFetch func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)

var detailFanOutTimeout = consensusTimeout

// fanOutByIdentity calls every provider that holds an ID for identity and
// returns the non-empty result groups. partial reports whether any provider the
// fan-out would have asked failed to answer: it errored, panicked, timed out,
// or was short-circuited by an open circuit. A provider with no ID for the
// artist was never going to contribute, so it does not make the answer partial.
func (s *GetArtistContentService) fanOutByIdentity(ctx context.Context, identity ResolvedArtistIdentity, artistName string, fetch identityContentFetch) (groups [][]domain.SearchResult, partial bool) {
	// The breaker judges outcomes against the caller's context: a provider cut
	// off by the fan-out deadline below is slow, not abandoned.
	callerCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, detailFanOutTimeout)
	defer cancel()

	type job struct {
		provider domain.ProviderName
		p        ports.ArtistContentProvider
		id       string
		call     breakerCall
	}
	var jobs []job
	for _, name := range orderedProviderNames(s.providers) {
		call, ok := admitProviderCall(s.breaker, name)
		if !ok {
			if providerContentID(identity, name) != "" || artistName != "" {
				partial = true
			}
			continue
		}
		p := s.providers[name]
		id := providerContentID(identity, name)
		if id == "" {
			id = resolveArtistIDByName(ctx, p, artistName)
		}
		if id == "" {
			call.release()
			continue
		}
		jobs = append(jobs, job{provider: name, p: p, id: id, call: call})
	}

	results := make([][]domain.SearchResult, len(jobs))
	failed := make([]bool, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			defer RecoverGoroutine(ctx, "artist_content.fanout.provider_panic", "provider", j.provider.String())
			// Set before the call and cleared on success, so a panic that
			// RecoverGoroutine swallows still counts as a failed provider.
			failed[i] = true
			settled := false
			defer j.call.failPanicked(&settled)
			res, err := fetch(ctx, j.p, j.provider, j.id)
			settled = true
			j.call.settle(callerCtx, err)
			if err != nil {
				errs[i] = err
				return
			}
			failed[i] = false
			results[i] = res
		}(i, j)
	}
	wg.Wait()

	groups = make([][]domain.SearchResult, 0, len(results))
	var failures []any
	for i, j := range jobs {
		if failed[i] {
			partial = true
		}
		if attr, ok := fanOutFailureAttr(callerCtx, j.provider, j.id, errs[i]); ok {
			failures = append(failures, attr)
		}
		if g := results[i]; len(g) > 0 {
			groups = append(groups, g)
		}
	}
	if len(failures) > 0 {
		slog.WarnContext(callerCtx, "artist_content.fanout.provider_failed", slog.Group("failed", failures...))
	}
	return groups, partial
}

// fanOutFailureAttr describes one provider's failed fan-out call for the
// per-fan-out Warn summary, keyed by provider name. One summary per fan-out
// rather than one line per provider keeps a widespread outage to a line per
// request. It reports false when there is nothing worth warning about: no
// error, or a call abandoned because the caller went away, which says nothing
// about the provider. Our own fan-out deadline still counts: that provider was
// slow. Circuit-open skips never reach here, and panics are logged at Error by
// RecoverGoroutine.
func fanOutFailureAttr(callerCtx context.Context, provider domain.ProviderName, externalID string, err error) (slog.Attr, bool) {
	if err == nil || callerCtx.Err() != nil || errors.Is(err, context.Canceled) {
		return slog.Attr{}, false
	}
	// A transport failure's *url.Error embeds the request URL, which for
	// LastFM and SoundCloud carries api_key / client_id.
	return slog.Group(provider.String(), "external_id", externalID, "error", redact.Secrets(err.Error())), true
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityStore != nil {
		identity, _ := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		artistRef := sourceKey(providerName, externalID)
		if albums, partial := s.v2Albums(ctx, identity, artistRef); len(albums) > 0 {
			resp := okContentResponse(providerName, albums, limit)
			resp.Partial = partial
			return resp, nil
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return unservedContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, s.breaker, providerName, externalID, "artist_albums.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistAlbums(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}

	results = dedupAlbums(results)

	if artistName != "" && s.consensus != nil {
		consensusResults := s.consensus.BuildConsensus(ctx, artistName, providerName, externalID, results)
		var kept []domain.SearchResult
		for _, cr := range consensusResults {
			if cr.Status != ConsensusRejected {
				kept = append(kept, cr.Album)
			}
		}
		if kept == nil {
			kept = []domain.SearchResult{}
		}
		results = kept
	}

	normalizeAlbumYears(results)
	sortByReleaseDateDesc(results, albumReleaseSortKey)

	return okContentResponse(providerName, results, limit), nil
}

var providerFanOutPriority = []domain.ProviderName{
	domain.CanonicalContentProvider,
	domain.ProviderAppleMusic,
	domain.ProviderSpotify,
	domain.ProviderITunes,
	domain.ProviderSoundCloud,
	domain.ProviderLastFM,
}

func orderedProviderNames(providers map[domain.ProviderName]ports.ArtistContentProvider) []domain.ProviderName {
	out := make([]domain.ProviderName, 0, len(providers))
	seen := make(map[domain.ProviderName]bool, len(providers))
	for _, name := range providerFanOutPriority {
		if _, ok := providers[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []domain.ProviderName
	for name := range providers {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].String() < rest[j].String() })
	return append(out, rest...)
}
