package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"
)

const albumFeaturedConcurrency = 5

type GetAlbumTracksService struct {
	providers        map[domain.ProviderName]ports.AlbumContentProvider
	featured         deezerFeaturedLookup
	fallbackSearcher ports.SearchProvider
	breaker          *CircuitBreaker
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

// WithAlbumCircuitBreaker gates every provider call the service makes through
// cb, the breaker shared with the search fan-out. Without it, calls are ungated.
func WithAlbumCircuitBreaker(cb *CircuitBreaker) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.breaker = cb }
}

func WithAlbumFallbackSearcher(sp ports.SearchProvider) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.fallbackSearcher = sp }
}

func (s *GetAlbumTracksService) enrichFeatured(ctx context.Context, results []domain.SearchResult) {
	if s.featured == nil {
		return
	}
	var g errgroup.Group
	g.SetLimit(albumFeaturedConcurrency)
	for i := range results {
		if results[i].Kind != domain.ResultKindTrack || len(results[i].Sources) == 0 {
			continue
		}
		src := results[i].Sources[0]
		if !domain.IsCanonicalContentProvider(src.Provider) || src.ExternalID == "" {
			continue
		}
		g.Go(func() error {
			defer RecoverGoroutine(ctx, "album_tracks.featured_panic", "external_id", src.ExternalID)
			feats, err := guardedFetch(ctx, s.breaker, domain.CanonicalContentProvider, func() ([]domain.FeaturedArtist, error) {
				return s.featured.LookupTrackFeatured(ctx, src.ExternalID)
			})
			if err != nil || len(feats) == 0 {
				return nil
			}
			results[i].PutExtra(domain.ExtraFeaturedArtists, domain.FeaturedArtistsToExtras(feats))
			return nil
		})
	}
	_ = g.Wait()
}

type AlbumTracksRequest struct {
	Provider     domain.ProviderName
	ExternalID   string
	Title        string
	Artist       string
	MBExternalID string
	Limit        int
}

func (s *GetAlbumTracksService) ExecuteRequest(ctx context.Context, req AlbumTracksRequest) (*ContentFetchResponse, error) {
	resp, err := s.fetchAlbumTracks(ctx, req.Provider, req.ExternalID, req.Title, req.Artist, req.Limit)
	if err != nil {
		return nil, err
	}
	s.mergeMusicBrainzFeaturing(ctx, req.MBExternalID, resp.Items)
	return resp, nil
}

func (s *GetAlbumTracksService) mergeMusicBrainzFeaturing(ctx context.Context, mbExternalID string, items []domain.SearchResult) {
	if mbExternalID == "" || len(items) == 0 {
		return
	}
	mb, ok := s.providers[domain.ProviderMusicBrainz]
	if !ok {
		return
	}
	mbTracks, err := guardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]domain.SearchResult, error) {
		return mb.GetAlbumTracks(ctx, domain.ProviderMusicBrainz, mbExternalID)
	})
	if err != nil || len(mbTracks) == 0 {
		return
	}

	featuredByTitle := make(map[string]any, len(mbTracks))
	for _, t := range mbTracks {
		if feats, present := t.Extras[domain.ExtraFeaturedArtists]; present {
			featuredByTitle[textnorm.NormalizeForMatch(t.Title)] = feats
		}
	}

	for i := range items {
		if _, present := items[i].Extras[domain.ExtraFeaturedArtists]; present {
			continue
		}
		feats, found := featuredByTitle[textnorm.NormalizeForMatch(items[i].Title)]
		if !found {
			continue
		}
		items[i].PutExtra(domain.ExtraFeaturedArtists, feats)
	}
}

func (s *GetAlbumTracksService) fetchAlbumTracks(ctx context.Context, providerName domain.ProviderName, externalID, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	var results []domain.SearchResult
	var degraded *ContentFetchResponse
	if provider, ok := s.providers[providerName]; ok {
		results, degraded = fetchProviderResults(ctx, s.breaker, providerName, externalID, "album_tracks.provider_failed",
			func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				return provider.GetAlbumTracks(ctx, pn, id)
			})
	} else {
		degraded = unservedContentResponse(providerName)
	}

	if degraded != nil || len(results) == 0 {
		if fallback := s.fallbackAlbumTracks(ctx, providerName, albumTitle, albumArtist, limit); fallback != nil {
			return fallback, nil
		}
		if degraded != nil {
			return degraded, nil
		}
	}

	resp := okContentResponse(providerName, results, limit)
	s.enrichFeatured(ctx, resp.Items)
	return resp, nil
}

// fallbackAlbumTracks is the substitute tracklist for a requested provider that
// had no answer, or nil when none is wired or none was found. Nil leaves the
// caller holding the requested provider's own failure, so an outage is never
// reported as a healthy empty album.
func (s *GetAlbumTracksService) fallbackAlbumTracks(ctx context.Context, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *ContentFetchResponse {
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

func (s *GetAlbumTracksService) deezerSearchFallback(ctx context.Context, deezer ports.AlbumContentProvider, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *ContentFetchResponse {
	query := albumSearchQuery(albumTitle, albumArtist)
	candidates, err := guardedFetch(ctx, s.breaker, s.fallbackSearcher.Name(), func() ([]domain.SearchResult, error) {
		return s.fallbackSearcher.Search(ctx, query, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	})
	if err != nil {
		// A transport failure's *url.Error embeds the request URL, which for
		// LastFM and SoundCloud carries api_key / client_id.
		slog.WarnContext(ctx, "album_tracks.deezer_fallback_failed",
			"requested_provider", requested.String(), "query", query, "error", redact.Secrets(err.Error()))
		return nil
	}

	tracks := s.firstMatchingTracklist(ctx, deezer, candidates, albumArtist)
	if len(tracks) == 0 {
		return nil
	}
	resp := fallbackContentResponse(domain.CanonicalContentProvider, requested, tracks, limit)
	s.enrichFeatured(ctx, resp.Items)
	slog.InfoContext(ctx, "album_tracks.deezer_fallback_served",
		"requested_provider", requested.String(), "query", query, "tracks", len(resp.Items))
	return resp
}

// firstMatchingTracklist is the tracklist of the first candidate album that is
// albumArtist's and has tracks, or nil when no candidate is.
func (s *GetAlbumTracksService) firstMatchingTracklist(ctx context.Context, deezer ports.AlbumContentProvider, candidates []domain.SearchResult, albumArtist string) []domain.SearchResult {
	wantArtist := textnorm.NormalizeForMatch(albumArtist)
	for _, candidate := range candidates {
		if len(candidate.Sources) == 0 {
			continue
		}
		if wantArtist != "" && textnorm.NormalizeForMatch(candidate.Subtitle) != wantArtist {
			continue
		}
		deezerAlbumID := candidate.Sources[0].ExternalID
		tracks, err := guardedFetch(ctx, s.breaker, domain.CanonicalContentProvider, func() ([]domain.SearchResult, error) {
			return deezer.GetAlbumTracks(ctx, domain.CanonicalContentProvider, deezerAlbumID)
		})
		if err != nil || len(tracks) == 0 {
			continue
		}
		return tracks
	}
	return nil
}
