package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

const albumFeaturedConcurrency = 5

type GetAlbumTracksService struct {
	providers        map[domain.ProviderName]ports.AlbumContentProvider
	featured         deezerFeaturedLookup
	fallbackSearcher ports.SearchProvider
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

type AlbumTracksRequest struct {
	Provider     domain.ProviderName
	ExternalID   string
	Title        string
	Artist       string
	MBExternalID string
	Limit        int
}

func (s *GetAlbumTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	return s.ExecuteRequest(ctx, AlbumTracksRequest{
		Provider:   providerName,
		ExternalID: externalID,
		Title:      albumTitle,
		Artist:     albumArtist,
		Limit:      limit,
	})
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
	mbTracks, err := mb.GetAlbumTracks(ctx, domain.ProviderMusicBrainz, mbExternalID)
	if err != nil || len(mbTracks) == 0 {
		return
	}

	featuredByTitle := make(map[string]any, len(mbTracks))
	for _, t := range mbTracks {
		if feats, present := t.Extras["featured_artists"]; present {
			featuredByTitle[textnorm.NormalizeForMatch(t.Title)] = feats
		}
	}

	for i := range items {
		if _, present := items[i].Extras["featured_artists"]; present {
			continue
		}
		feats, found := featuredByTitle[textnorm.NormalizeForMatch(items[i].Title)]
		if !found {
			continue
		}
		if items[i].Extras == nil {
			items[i].Extras = map[string]any{}
		}
		items[i].Extras["featured_artists"] = feats
	}
}

func (s *GetAlbumTracksService) fetchAlbumTracks(ctx context.Context, providerName domain.ProviderName, externalID, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	var results []domain.SearchResult
	var degraded *ContentFetchResponse
	if provider, ok := s.providers[providerName]; ok {
		results, degraded = fetchProviderResults(ctx, providerName, externalID, "album_tracks.provider_failed",
			func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				return provider.GetAlbumTracks(ctx, pn, id)
			})
	} else {
		degraded = errorContentResponse(providerName)
	}

	if degraded != nil || len(results) == 0 {
		if albumTitle != "" && s.fallbackSearcher != nil {
			if deezer, hasDeezer := s.providers[domain.ProviderDeezer]; hasDeezer {
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

	wantArtist := textnorm.NormalizeForMatch(albumArtist)
	for _, r := range results {
		if len(r.Sources) == 0 {
			continue
		}
		if wantArtist != "" && textnorm.NormalizeForMatch(r.Subtitle) != wantArtist {
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
