package artistpage

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"strings"
)

type GetAlbumTracksService struct {
	providers map[domain.ProviderName]ports.AlbumContentProvider
	featured  FeaturedEnrichment
	fallback  AlbumFallback
	breaker   *providerguard.CircuitBreaker
}

func NewGetAlbumTracksService(
	providers map[domain.ProviderName]ports.AlbumContentProvider,
	featured FeaturedEnrichment,
	fallback AlbumFallback,
	breaker *providerguard.CircuitBreaker,
) (*GetAlbumTracksService, error) {
	if err := requireCollaborators("the album tracks service",
		collaborator{"providers", providers == nil},
		collaborator{"a featured enrichment", featured == nil},
		collaborator{"an album fallback", fallback == nil},
		collaborator{"a circuit breaker", breaker == nil},
	); err != nil {
		return nil, err
	}
	return &GetAlbumTracksService{providers: providers, featured: featured, fallback: fallback, breaker: breaker}, nil
}

type AlbumTracksRequest struct {
	Provider              domain.ProviderName
	ExternalID            string
	Title                 string
	Artist                string
	MusicBrainzExternalID string
	Limit                 int
	Known                 map[domain.ProviderName]string
}

func (s *GetAlbumTracksService) ExecuteRequest(ctx context.Context, req AlbumTracksRequest) (*providerguard.ContentFetchResponse, error) {
	resp, err := s.fetchAlbumTracks(ctx, req.Provider, req.ExternalID, req.Title, req.Artist, req.Limit)
	if err != nil {
		return nil, err
	}
	s.featured.enrich(ctx, resp.Items, s.featuredFromSpotify(ctx, req.Known, resp))
	s.mergeMusicBrainzFeaturing(ctx, req.MusicBrainzExternalID, resp.Items)
	return resp, nil
}

func (s *GetAlbumTracksService) featuredFromSpotify(ctx context.Context, known map[domain.ProviderName]string, resp *providerguard.ContentFetchResponse) (answered map[int]bool) {
	if resp.ProviderName == domain.ProviderSpotify {
		return allTracks(resp.Items)
	}
	spotify, ok := s.providers[domain.ProviderSpotify]
	albumID := known[domain.ProviderSpotify]
	if !ok || albumID == "" || len(resp.Items) == 0 {
		return nil
	}
	tracks, err := providerguard.GuardedFetch(ctx, s.breaker, domain.ProviderSpotify, func() ([]domain.SearchResult, error) {
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
			to[i] = to[i].WithExtra(domain.ExtraFeaturedArtists, feats)
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
	mbTracks, err := providerguard.GuardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]domain.SearchResult, error) {
		return mb.GetAlbumTracks(ctx, domain.ProviderMusicBrainz, mbExternalID)
	})
	if err != nil || len(mbTracks) == 0 {
		return
	}
	_ = copyFeaturedByTitle(mbTracks, items)
}

func (s *GetAlbumTracksService) fetchAlbumTracks(ctx context.Context, providerName domain.ProviderName, externalID, albumTitle, albumArtist string, limit int) (*providerguard.ContentFetchResponse, error) {
	results, partial, degraded := s.fetchFromProvider(ctx, providerName, externalID)
	if degraded != nil || len(results) == 0 {
		if fallback := s.fallbackAlbumTracks(ctx, providerName, albumTitle, albumArtist, limit); fallback != nil {
			return fallback, nil
		}
		if degraded != nil {
			return degraded, nil
		}
	}

	resp := providerguard.OkContentResponse(providerName, results, limit)
	resp.Partial = partial
	return resp, nil
}

func (s *GetAlbumTracksService) fetchFromProvider(ctx context.Context, providerName domain.ProviderName, externalID string) ([]domain.SearchResult, bool, *providerguard.ContentFetchResponse) {
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, false, providerguard.UnservedContentResponse(providerName)
	}
	return providerguard.FetchProviderContent(ctx, s.breaker, providerName, externalID, "album_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetAlbumTracks(ctx, pn, id)
		})
}

func (s *GetAlbumTracksService) fallbackAlbumTracks(ctx context.Context, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *providerguard.ContentFetchResponse {
	if albumTitle == "" {
		return nil
	}
	deezer, hasDeezer := s.providers[domain.CanonicalContentProvider]
	if !hasDeezer {
		return nil
	}
	return s.fallback.tracks(ctx, deezer, requested, albumTitle, albumArtist, limit)
}
