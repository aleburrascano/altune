package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"golang.org/x/sync/errgroup"
)

const albumFeaturedConcurrency = 5

// Album tracklists don't carry contributors inline, so we fetch each track's
// featured artists individually via the Deezer adapter's LookupTrackFeatured
// (deezerFeaturedLookup, declared in featured_resolver.go).
type GetAlbumTracksService struct {
	providers        map[string]ports.AlbumContentProvider
	featured         deezerFeaturedLookup
	fallbackSearcher ports.SearchProvider
}

type AlbumTracksOption func(*GetAlbumTracksService)

func NewGetAlbumTracksService(
	providers map[string]ports.AlbumContentProvider,
	opts ...AlbumTracksOption,
) *GetAlbumTracksService {
	s := &GetAlbumTracksService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithTrackFeatured enables per-track featured-artist enrichment of album tracks.
func WithTrackFeatured(f deezerFeaturedLookup) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.featured = f }
}

// WithAlbumFallbackSearcher wires the SearchProvider used by the Deezer
// search-then-fetch fallback (see deezerSearchFallback). Without this the
// fallback never fires, which is the correct default for tests that only
// exercise the primary provider path.
func WithAlbumFallbackSearcher(sp ports.SearchProvider) AlbumTracksOption {
	return func(s *GetAlbumTracksService) { s.fallbackSearcher = sp }
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


func (s *GetAlbumTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName.String()]
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "album_tracks.provider_failed", ok,
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetAlbumTracks(ctx, pn, id)
		})

	// Fallback: an unsupported/failing provider, or one that resolved zero
	// tracks, falls back to a Deezer album search by title+artist when Deezer is
	// available. Orthogonal to the found/parse/fetch shape fetchProviderResults
	// owns, so it stays here rather than in the shared helper.
	if degraded != nil || len(results) == 0 {
		if albumTitle != "" && s.fallbackSearcher != nil {
			if deezer, hasDeezer := s.providers["deezer"]; hasDeezer {
				return s.deezerSearchFallback(ctx, deezer, albumTitle, albumArtist, limit)
			}
		}
		if degraded != nil {
			return degraded, nil
		}
	}

	resp := okContentResponse(providerName, results, limit)
	s.enrichFeatured(ctx, resp.Items)
	return resp, nil
}

func (s *GetAlbumTracksService) deezerSearchFallback(ctx context.Context, deezer ports.AlbumContentProvider, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	query := albumTitle
	if albumArtist != "" {
		query = albumArtist + " " + albumTitle
	}

	results, err := s.fallbackSearcher.Search(ctx, query, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.deezer_fallback_failed",
			"query", query, "error", err)
	}
	if err != nil || len(results) == 0 {
		return emptyContentResponse(domain.ProviderDeezer), nil
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
		resp := okContentResponse(domain.ProviderDeezer, tracks, limit)
		s.enrichFeatured(ctx, resp.Items)
		return resp, nil
	}

	return emptyContentResponse(domain.ProviderDeezer), nil
}
