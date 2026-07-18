package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"golang.org/x/sync/errgroup"
)

// trackFeaturedLookup fetches a track's featured artists by its provider id. The
// Deezer adapter satisfies it (LookupTrackFeatured). Album tracklists don't carry
// contributors inline, so we fetch them per track to populate featured_artists.
type trackFeaturedLookup interface {
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

const albumFeaturedConcurrency = 5

type GetAlbumTracksService struct {
	providers map[string]ports.AlbumContentProvider
	featured  trackFeaturedLookup
}

func NewGetAlbumTracksService(
	providers map[string]ports.AlbumContentProvider,
	opts ...func(*GetAlbumTracksService),
) *GetAlbumTracksService {
	s := &GetAlbumTracksService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithTrackFeatured enables per-track featured-artist enrichment of album tracks.
func WithTrackFeatured(f trackFeaturedLookup) func(*GetAlbumTracksService) {
	return func(s *GetAlbumTracksService) { s.featured = f }
}

// enrichFeatured fetches each Deezer-sourced track's featured contributors
// concurrently (bounded) and stamps them into Extras["featured_artists"]. A
// per-track failure degrades that track to no features rather than failing the
// whole tracklist. Each goroutine writes a distinct slice index, so no shared map.
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
		if src.Provider != domain.ProviderDeezer || src.ExternalID == "" {
			continue
		}
		g.Go(func() error {
			feats, err := s.featured.LookupTrackFeatured(ctx, src.ExternalID)
			if err != nil || len(feats) == 0 {
				return nil
			}
			if results[i].Extras == nil {
				results[i].Extras = map[string]any{}
			}
			results[i].Extras["featured_artists"] = domain.FeaturedArtistsToExtras(feats)
			return nil
		})
	}
	_ = g.Wait()
}

type ContentFetchResponse struct {
	ProviderName string
	Status       domain.ProviderStatus
	Items        []domain.SearchResult
}

// errorContentResponse is the degraded envelope every content use case returns
// when a provider is missing, unparseable, or fails: an error status with a
// non-nil empty item slice (so the wire serializes [] rather than null).
func errorContentResponse(providerName string) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusError,
		Items:        []domain.SearchResult{},
	}
}

// emptyContentResponse is the OK-but-nothing-found envelope: a healthy fetch that
// produced no items, again with a non-nil empty slice for the wire.
func emptyContentResponse(providerName string) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        []domain.SearchResult{},
	}
}

func (s *GetAlbumTracksService) Execute(ctx context.Context, providerName, externalID, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		// Fallback: if the requested provider isn't supported but Deezer is,
		// search Deezer for this album by title+artist and return those tracks.
		deezer, hasDeezer := s.providers["deezer"]
		if hasDeezer && albumTitle != "" {
			return s.deezerSearchFallback(ctx, deezer, albumTitle, albumArtist, limit)
		}
		return errorContentResponse(providerName), nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return errorContentResponse(providerName), nil
	}
	results, err := provider.GetAlbumTracks(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
	}
	if err != nil || len(results) == 0 {
		if albumTitle != "" {
			deezer, hasDeezer := s.providers["deezer"]
			if hasDeezer {
				return s.deezerSearchFallback(ctx, deezer, albumTitle, albumArtist, limit)
			}
		}
		if err != nil {
			return errorContentResponse(providerName), nil
		}
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	s.enrichFeatured(ctx, results)

	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}, nil
}

func (s *GetAlbumTracksService) deezerSearchFallback(ctx context.Context, deezer ports.AlbumContentProvider, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	searcher, ok := deezer.(ports.SearchProvider)
	if !ok {
		return errorContentResponse("deezer"), nil
	}

	query := albumTitle
	if albumArtist != "" {
		query = albumArtist + " " + albumTitle
	}

	results, err := searcher.Search(ctx, query, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.deezer_fallback_failed",
			"query", query, "error", err)
	}
	if err != nil || len(results) == 0 {
		return emptyContentResponse("deezer"), nil
	}

	// Use the first matching album's Deezer ID to fetch tracks
	for _, r := range results {
		if len(r.Sources) == 0 {
			continue
		}
		deezerAlbumID := r.Sources[0].ExternalID
		tracks, err := deezer.GetAlbumTracks(ctx, domain.ProviderDeezer, deezerAlbumID)
		if err != nil || len(tracks) == 0 {
			continue
		}
		if limit > 0 && len(tracks) > limit {
			tracks = tracks[:limit]
		}
		s.enrichFeatured(ctx, tracks)
		return &ContentFetchResponse{
			ProviderName: "deezer",
			Status:       domain.ProviderStatusOK,
			Items:        tracks,
		}, nil
	}

	return emptyContentResponse("deezer"), nil
}
