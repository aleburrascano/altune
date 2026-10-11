package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/release"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
)

type GetArtistContentService struct {
	providers            map[domain.ProviderName]ports.ArtistContentProvider
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

func WithContentIdentityStore(store ports.IdentityStore) ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityStore = store }
}

func WithContentCircuitBreaker(cb *CircuitBreaker) ArtistContentOption {
	return func(s *GetArtistContentService) { s.breaker = cb }
}

func WithMBAnchor(anchor ports.MBDiscographyAnchor) ArtistContentOption {
	return func(s *GetArtistContentService) { s.mbAnchor = anchor }
}

func WithContentEventStore(eventStore ports.EventStore) ArtistContentOption {
	return func(s *GetArtistContentService) {
		if eventStore != nil {
			s.discographyTelemetry = newDiscographyTelemetry(eventStore)
		}
	}
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName domain.ProviderName, externalID string, known map[domain.ProviderName]string, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.usesIdentity(known) {
		tracks, partial := s.v2TopTracks(ctx, s.artistIdentity(ctx, providerName, externalID, known))
		return s.identityAnswer(providerName, tracks, partial, limit), nil
	}
	return s.seedTopTracks(ctx, providerName, externalID, limit), nil
}

func (s *GetArtistContentService) usesIdentity(known map[domain.ProviderName]string) bool {
	return s.identityStore != nil || len(known) > 0
}

func (s *GetArtistContentService) identityAnswer(providerName domain.ProviderName, items []domain.SearchResult, partial bool, limit int) *ContentFetchResponse {
	if _, served := s.providers[providerName]; !served && len(items) == 0 {
		return unservedContentResponse(providerName)
	}
	return withPartial(okContentResponse(providerName, items, limit), partial)
}

func withPartial(resp *ContentFetchResponse, partial bool) *ContentFetchResponse {
	resp.Partial = partial
	return resp
}

func (s *GetArtistContentService) seedTopTracks(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) *ContentFetchResponse {
	provider, ok := s.providers[providerName]
	if !ok {
		return unservedContentResponse(providerName)
	}
	results, partial, degraded := fetchProviderContent(ctx, s.breaker, providerName, externalID, "artist_top_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistTopTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded
	}
	return withPartial(okContentResponse(providerName, results, limit), partial)
}

type identityContentFetch func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)

var detailFanOutTimeout = consensusTimeout

type identityJob struct {
	provider domain.ProviderName
	p        ports.ArtistContentProvider
	id       string
	call     breakerCall
}

func (s *GetArtistContentService) admitIdentityJobs(ctx context.Context, identity ResolvedArtistIdentity) (jobs []identityJob, partial bool) {
	for _, name := range orderedProviderNames(s.providers) {
		call, ok := admitProviderCall(ctx, s.breaker, name)
		id := providerContentID(identity, name)
		if !ok {
			if id != "" {
				partial = true
			}
			continue
		}
		if id == "" {
			call.release()
			continue
		}
		jobs = append(jobs, identityJob{provider: name, p: s.providers[name], id: id, call: call})
	}
	return jobs, partial
}

func (s *GetArtistContentService) fanOutByIdentity(ctx context.Context, identity ResolvedArtistIdentity, fetch identityContentFetch) (groups [][]domain.SearchResult, partial bool) {
	callerCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, detailFanOutTimeout)
	defer cancel()

	jobs, partial := s.admitIdentityJobs(callerCtx, identity)

	outcomes := runIdentityJobs(ctx, callerCtx, jobs, fetch)
	groups, jobsPartial := collectIdentityOutcomes(callerCtx, jobs, outcomes)
	return groups, partial || jobsPartial
}

type identityOutcome struct {
	results   []domain.SearchResult
	failed    bool
	truncated bool
	err       error
}

func runIdentityJobs(ctx, callerCtx context.Context, jobs []identityJob, fetch identityContentFetch) []identityOutcome {
	outcomes := make([]identityOutcome, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j identityJob) {
			defer wg.Done()
			defer RecoverGoroutine(ctx, "artist_content.fanout.provider_panic", "provider", j.provider.String())
			outcomes[i].failed = true
			outcomes[i] = runIdentityJob(ctx, callerCtx, j, fetch)
		}(i, j)
	}
	wg.Wait()
	return outcomes
}

func runIdentityJob(ctx, callerCtx context.Context, j identityJob, fetch identityContentFetch) identityOutcome {
	res, err := runAdmitted(callerCtx, j.call, breakerOutcome,
		func() ([]domain.SearchResult, error) { return fetch(ctx, j.p, j.provider, j.id) })
	o := identityOutcome{results: res, err: err}
	if isPartialResult(err) {
		o.truncated = true
		o.err = nil
	}
	o.failed = o.err != nil
	return o
}

func collectIdentityOutcomes(callerCtx context.Context, jobs []identityJob, outcomes []identityOutcome) (groups [][]domain.SearchResult, partial bool) {
	groups = make([][]domain.SearchResult, 0, len(outcomes))
	var failures []any
	for i, j := range jobs {
		o := outcomes[i]
		if o.failed || o.truncated {
			partial = true
		}
		if attr, ok := fanOutFailureAttr(callerCtx, j.provider, j.id, o.err); ok {
			failures = append(failures, attr)
		}
		if len(o.results) > 0 {
			groups = append(groups, o.results)
		}
	}
	if len(failures) > 0 {
		slog.WarnContext(callerCtx, "artist_content.fanout.provider_failed", slog.Group("failed", failures...))
	}
	return groups, partial
}

func fanOutFailureAttr(callerCtx context.Context, provider domain.ProviderName, externalID string, err error) (slog.Attr, bool) {
	if err == nil || callerCtx.Err() != nil || errors.Is(err, context.Canceled) {
		return slog.Attr{}, false
	}
	return slog.Group(provider.String(), "external_id", externalID, "error", redact.Secrets(err.Error())), true
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName domain.ProviderName, externalID string, known map[domain.ProviderName]string, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.usesIdentity(known) {
		identity := s.artistIdentity(ctx, providerName, externalID, known)
		albums, partial := s.v2Albums(ctx, identity, release.SourceKey(providerName, externalID))
		return s.identityAnswer(providerName, albums, partial, limit), nil
	}
	return s.seedAlbums(ctx, providerName, externalID, limit), nil
}

func (s *GetArtistContentService) seedAlbums(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) *ContentFetchResponse {
	provider, ok := s.providers[providerName]
	if !ok {
		return unservedContentResponse(providerName)
	}
	results, partial, degraded := fetchProviderContent(ctx, s.breaker, providerName, externalID, "artist_albums.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistAlbums(ctx, pn, id)
		})
	if degraded != nil {
		return degraded
	}
	results = dedupAlbums(results)
	normalizeAlbumYears(results)
	sortByReleaseDateDesc(results, albumReleaseSortKey)
	return withPartial(okContentResponse(providerName, results, limit), partial)
}

var providerFanOutPriority = []domain.ProviderName{
	domain.CanonicalContentProvider,
	domain.ProviderAppleMusic,
	domain.ProviderSpotify,
	domain.ProviderITunes,
	domain.ProviderSoundCloud,
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
