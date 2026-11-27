package artistpage

import (
	"altune/go-api/internal/search/internal/core/albumgrouping"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/parallel"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"
)

type GetArtistContentService struct {
	providers   map[domain.ProviderName]ports.ArtistContentProvider
	tracklists  map[domain.ProviderName]ports.AlbumContentProvider
	identity    ArtistIdentity
	anchoring   Anchoring
	discography DiscographyObserver
	breaker     *providerguard.CircuitBreaker
}

func NewGetArtistContentService(
	providers map[domain.ProviderName]ports.ArtistContentProvider,
	tracklists map[domain.ProviderName]ports.AlbumContentProvider,
	identity ArtistIdentity,
	anchoring Anchoring,
	discography DiscographyObserver,
	breaker *providerguard.CircuitBreaker,
) (*GetArtistContentService, error) {
	if err := requireCollaborators("the artist content service",
		collaborator{"providers", providers == nil},
		collaborator{"tracklist providers", tracklists == nil},
		collaborator{"an artist identity", identity == nil},
		collaborator{"an anchoring strategy", anchoring == nil},
		collaborator{"a discography observer", discography == nil},
		collaborator{"a circuit breaker", breaker == nil},
	); err != nil {
		return nil, err
	}
	return &GetArtistContentService{
		providers:   providers,
		tracklists:  tracklists,
		identity:    identity,
		anchoring:   anchoring,
		discography: discography,
		breaker:     breaker,
	}, nil
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName domain.ProviderName, externalID string, known map[domain.ProviderName]string, limit int) (*providerguard.ContentFetchResponse, error) {
	if identity, serves := s.identity.Identify(ctx, providerName, externalID, known); serves {
		tracks, partial := s.v2TopTracks(ctx, identity)
		return s.identityAnswer(providerName, tracks, partial, limit), nil
	}
	return s.seedTopTracks(ctx, providerName, externalID, limit), nil
}

func (s *GetArtistContentService) identityAnswer(providerName domain.ProviderName, items []domain.SearchResult, partial bool, limit int) *providerguard.ContentFetchResponse {
	if _, served := s.providers[providerName]; !served && len(items) == 0 {
		return providerguard.UnservedContentResponse(providerName)
	}
	return withPartial(providerguard.OkContentResponse(providerName, items, limit), partial)
}

func withPartial(resp *providerguard.ContentFetchResponse, partial bool) *providerguard.ContentFetchResponse {
	resp.Partial = partial
	return resp
}

func (s *GetArtistContentService) seedTopTracks(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) *providerguard.ContentFetchResponse {
	provider, ok := s.providers[providerName]
	if !ok {
		return providerguard.UnservedContentResponse(providerName)
	}
	results, partial, degraded := providerguard.FetchProviderContent(ctx, s.breaker, providerName, externalID, "artist_top_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistTopTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded
	}
	return withPartial(providerguard.OkContentResponse(providerName, results, limit), partial)
}

type identityContentFetch func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)

const detailFanOutTimeout = 10 * time.Second

type identityJob struct {
	provider domain.ProviderName
	p        ports.ArtistContentProvider
	id       string
	call     providerguard.BreakerCall
}

func (s *GetArtistContentService) admitIdentityJobs(ctx context.Context, identity ResolvedArtistIdentity) (jobs []identityJob, partial bool) {
	for _, name := range orderedProviderNames(s.providers) {
		call, ok := providerguard.AdmitProviderCall(ctx, s.breaker, name)
		id := providerContentID(identity, name)
		if !ok {
			if id != "" {
				partial = true
			}
			continue
		}
		if id == "" {
			call.Release()
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
	g := parallel.New("artist_content.fanout.provider_panic", len(jobs))
	for i, j := range jobs {
		outcomes[i].failed = true
		g.Go(ctx, func() { outcomes[i] = runIdentityJob(ctx, callerCtx, j, fetch) }, "provider", j.provider.String())
	}
	_ = g.Wait()
	return outcomes
}

func runIdentityJob(ctx, callerCtx context.Context, j identityJob, fetch identityContentFetch) identityOutcome {
	res, err := providerguard.RunAdmitted(callerCtx, j.call, providerguard.BreakerOutcome,
		func() ([]domain.SearchResult, error) { return fetch(ctx, j.p, j.provider, j.id) })
	o := identityOutcome{results: res, err: err}
	if providerguard.IsPartialResult(err) {
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
	return slog.Group(provider.String(), "external_id", externalID, logging.KeyError, redact.Secrets(err.Error())), true
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName domain.ProviderName, externalID string, known map[domain.ProviderName]string, limit int) (*providerguard.ContentFetchResponse, error) {
	if identity, serves := s.identity.Identify(ctx, providerName, externalID, known); serves {
		albums, partial := s.v2Albums(ctx, identity, albumgrouping.SourceKey(providerName, externalID))
		return s.identityAnswer(providerName, albums, partial, limit), nil
	}
	return s.seedAlbums(ctx, providerName, externalID, limit), nil
}

func (s *GetArtistContentService) seedAlbums(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) *providerguard.ContentFetchResponse {
	provider, ok := s.providers[providerName]
	if !ok {
		return providerguard.UnservedContentResponse(providerName)
	}
	results, partial, degraded := providerguard.FetchProviderContent(ctx, s.breaker, providerName, externalID, "artist_albums.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistAlbums(ctx, pn, id)
		})
	if degraded != nil {
		return degraded
	}
	results = domain.DedupAlbums(results)
	domain.NormalizeAlbumYears(results)
	domain.SortByReleaseDateDesc(results, domain.AlbumReleaseSortKey)
	return withPartial(providerguard.OkContentResponse(providerName, results, limit), partial)
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
