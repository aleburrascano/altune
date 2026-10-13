package content

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/shared/fanout"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/runloop"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"strings"
)

const albumFeaturedConcurrency = 5

type GetAlbumTracksService struct {
	providers        map[domain.ProviderName]ports.AlbumContentProvider
	featured         deezerFeaturedLookup
	fallbackSearcher ports.SearchProvider
	breaker          *providercall.CircuitBreaker
}

type AlbumTracksOption func(*GetAlbumTracksService)

func NewGetAlbumTracksService(
	providers map[domain.ProviderName]ports.AlbumContentProvider,
	opts ...AlbumTracksOption,
) *GetAlbumTracksService {
	s := &GetAlbumTracksService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithTrackFeatured(f deezerFeaturedLookup) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.featured = f }
}

func WithAlbumCircuitBreaker(cb *providercall.CircuitBreaker) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.breaker = cb }
}

func WithAlbumFallbackSearcher(sp ports.SearchProvider) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.fallbackSearcher = sp }
}

func (s *GetAlbumTracksService) enrichFeatured(ctx context.Context, results []domain.SearchResult, answered map[int]bool) {
	if s.featured == nil {
		return
	}
	g := fanout.New(albumFeaturedConcurrency)
	for i := range results {
		if !answered[i] && hasDeezerTrackID(results[i]) {
			g.Go(func() {
				s.lookupFeatured(ctx, &results[i])
			})
		}
	}
	g.Wait()
}

func hasDeezerTrackID(r domain.SearchResult) bool {
	return r.Kind == domain.ResultKindTrack && len(r.Sources) > 0 &&
		domain.IsCanonicalContentProvider(r.Sources[0].Provider) && r.Sources[0].ExternalID != ""
}

func (s *GetAlbumTracksService) lookupFeatured(ctx context.Context, track *domain.SearchResult) {
	trackID := track.Sources[0].ExternalID
	defer runloop.RecoverContext(ctx, "album_tracks.featured_panic", "external_id", trackID)
	feats, err := providercall.GuardedFetch(ctx, s.breaker, domain.CanonicalContentProvider, func() ([]domain.FeaturedArtist, error) {
		return s.featured.LookupTrackFeatured(ctx, trackID)
	})
	if err == nil && len(feats) > 0 {
		track.PutExtra(domain.ExtraFeaturedArtists, domain.FeaturedArtistsToExtras(feats))
	}
}

type AlbumTracksRequest struct {
	Provider     domain.ProviderName
	ExternalID   string
	Title        string
	Artist       string
	MBExternalID string
	Limit        int
	Known        map[domain.ProviderName]string
}

func (s *GetAlbumTracksService) ExecuteRequest(ctx context.Context, req AlbumTracksRequest) (*providercall.ContentFetchResponse, error) {
	resp, err := s.fetchAlbumTracks(ctx, req.Provider, req.ExternalID, req.Title, req.Artist, req.Limit)
	if err != nil {
		return nil, err
	}
	s.enrichFeatured(ctx, resp.Items, s.featuredFromSpotify(ctx, req.Known, resp))
	s.mergeMusicBrainzFeaturing(ctx, req.MBExternalID, resp.Items)
	return resp, nil
}

func (s *GetAlbumTracksService) featuredFromSpotify(ctx context.Context, known map[domain.ProviderName]string, resp *providercall.ContentFetchResponse) (answered map[int]bool) {
	if resp.ProviderName == domain.ProviderSpotify {
		return allTracks(resp.Items)
	}
	spotify, ok := s.providers[domain.ProviderSpotify]
	albumID := known[domain.ProviderSpotify]
	if !ok || albumID == "" || len(resp.Items) == 0 {
		return nil
	}
	tracks, err := providercall.GuardedFetch(ctx, s.breaker, domain.ProviderSpotify, func() ([]domain.SearchResult, error) {
		return spotify.GetAlbumTracks(ctx, domain.ProviderSpotify, albumID)
	})
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.spotify_featured_failed",
			"album_id", albumID, "error", redact.Secrets(err.Error()))
		return nil
	}
	return copyFeaturedByTitle(tracks, resp.Items)
}

func allTracks(items []domain.SearchResult) map[int]bool {
	all := make(map[int]bool, len(items))
	for i := range items {
		all[i] = true
	}
	return all
}

func versionlessTitle(title string) string {
	base, _, _ := strings.Cut(title, " - ")
	return textnorm.NormalizeForMatch(base)
}

type featuredByTitle struct {
	exact       map[string]any
	versionless map[string]any
}

func indexFeaturedByTitle(tracks []domain.SearchResult) featuredByTitle {
	idx := featuredByTitle{exact: map[string]any{}, versionless: map[string]any{}}
	for _, t := range tracks {
		feats := t.Extras[domain.ExtraFeaturedArtists]
		idx.exact[textnorm.NormalizeForMatch(t.Title)] = feats
		if _, taken := idx.versionless[versionlessTitle(t.Title)]; !taken {
			idx.versionless[versionlessTitle(t.Title)] = feats
		}
	}
	return idx
}

func (idx featuredByTitle) lookup(title string) (any, bool) {
	if feats, found := idx.exact[textnorm.NormalizeForMatch(title)]; found {
		return feats, true
	}
	feats, found := idx.versionless[versionlessTitle(title)]
	return feats, found
}

func copyFeaturedByTitle(from, to []domain.SearchResult) (matched map[int]bool) {
	idx := indexFeaturedByTitle(from)
	matched = make(map[int]bool, len(to))
	for i := range to {
		feats, found := idx.lookup(to[i].Title)
		if !found {
			continue
		}
		matched[i] = true
		if _, present := to[i].Extras[domain.ExtraFeaturedArtists]; !present && feats != nil {
			to[i].PutExtra(domain.ExtraFeaturedArtists, feats)
		}
	}
	return matched
}

func (s *GetAlbumTracksService) mergeMusicBrainzFeaturing(ctx context.Context, mbExternalID string, items []domain.SearchResult) {
	if mbExternalID == "" || len(items) == 0 {
		return
	}
	mb, ok := s.providers[domain.ProviderMusicBrainz]
	if !ok {
		return
	}
	mbTracks, err := providercall.GuardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]domain.SearchResult, error) {
		return mb.GetAlbumTracks(ctx, domain.ProviderMusicBrainz, mbExternalID)
	})
	if err != nil || len(mbTracks) == 0 {
		return
	}
	_ = copyFeaturedByTitle(mbTracks, items)
}

func (s *GetAlbumTracksService) fetchAlbumTracks(ctx context.Context, providerName domain.ProviderName, externalID, albumTitle, albumArtist string, limit int) (*providercall.ContentFetchResponse, error) {
	results, partial, degraded := s.fetchFromProvider(ctx, providerName, externalID)
	if degraded != nil || len(results) == 0 {
		if fallback := s.fallbackAlbumTracks(ctx, providerName, albumTitle, albumArtist, limit); fallback != nil {
			return fallback, nil
		}
		if degraded != nil {
			return degraded, nil
		}
	}

	resp := providercall.OkContentResponse(providerName, results, limit)
	resp.Partial = partial
	return resp, nil
}

func (s *GetAlbumTracksService) fetchFromProvider(ctx context.Context, providerName domain.ProviderName, externalID string) ([]domain.SearchResult, bool, *providercall.ContentFetchResponse) {
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, false, providercall.UnservedContentResponse(providerName)
	}
	return providercall.FetchProviderContent(ctx, s.breaker, providerName, externalID, "album_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetAlbumTracks(ctx, pn, id)
		})
}

func (s *GetAlbumTracksService) fallbackAlbumTracks(ctx context.Context, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *providercall.ContentFetchResponse {
	if albumTitle == "" || s.fallbackSearcher == nil {
		return nil
	}
	deezer, hasDeezer := s.providers[domain.CanonicalContentProvider]
	if !hasDeezer {
		return nil
	}
	return s.deezerSearchFallback(ctx, deezer, requested, albumTitle, albumArtist, limit)
}

func albumSearchQuery(albumTitle, albumArtist string) string {
	if albumArtist == "" {
		return albumTitle
	}
	return albumArtist + " " + albumTitle
}

func (s *GetAlbumTracksService) deezerSearchFallback(ctx context.Context, deezer ports.AlbumContentProvider, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *providercall.ContentFetchResponse {
	query := albumSearchQuery(albumTitle, albumArtist)
	candidates, err := providercall.GuardedFetch(ctx, s.breaker, s.fallbackSearcher.Name(), func() ([]domain.SearchResult, error) {
		return s.fallbackSearcher.Search(ctx, query, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	})
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.deezer_fallback_failed",
			"requested_provider", requested.String(), "query", query, "error", redact.Secrets(err.Error()))
		return nil
	}

	tracks := s.firstMatchingTracklist(ctx, deezer, candidates, albumTitle, albumArtist)
	if len(tracks) == 0 {
		return nil
	}
	resp := providercall.FallbackContentResponse(domain.CanonicalContentProvider, requested, tracks, limit)
	slog.InfoContext(ctx, "album_tracks.deezer_fallback_served",
		"requested_provider", requested.String(), "query", query, "tracks", len(resp.Items))
	return resp
}

func isRequestedAlbum(candidate domain.SearchResult, albumTitle, albumArtist string) bool {
	if len(candidate.Sources) == 0 {
		return false
	}
	if textnorm.NormalizeForMatch(candidate.Title) != textnorm.NormalizeForMatch(albumTitle) {
		return false
	}
	wantArtist := textnorm.NormalizeForMatch(albumArtist)
	return wantArtist == "" || textnorm.NormalizeForMatch(candidate.Subtitle) == wantArtist
}

func (s *GetAlbumTracksService) firstMatchingTracklist(ctx context.Context, deezer ports.AlbumContentProvider, candidates []domain.SearchResult, albumTitle, albumArtist string) []domain.SearchResult {
	for _, candidate := range candidates {
		if !isRequestedAlbum(candidate, albumTitle, albumArtist) {
			continue
		}
		deezerAlbumID := candidate.Sources[0].ExternalID
		tracks, err := providercall.GuardedFetch(ctx, s.breaker, domain.CanonicalContentProvider, func() ([]domain.SearchResult, error) {
			return deezer.GetAlbumTracks(ctx, domain.CanonicalContentProvider, deezerAlbumID)
		})
		if err != nil || len(tracks) == 0 {
			continue
		}
		return tracks
	}
	return nil
}
