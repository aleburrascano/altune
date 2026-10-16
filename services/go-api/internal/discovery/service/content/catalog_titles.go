package content

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"context"
)

var _ ports.ArtistCatalogTitles = (*GetArtistContentService)(nil)

const (
	catalogTitlesTrackLimit = 50
	catalogTitlesAlbumLimit = 200
)

func (s *GetArtistContentService) CatalogTitles(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) ([]string, error) {
	tracks, err := s.GetTopTracks(ctx, provider, externalID, known, catalogTitlesTrackLimit)
	if err != nil {
		return nil, err
	}
	albums, err := s.GetAlbums(ctx, provider, externalID, known, catalogTitlesAlbumLimit)
	if err != nil {
		return nil, err
	}
	var items []domain.SearchResult
	for _, resp := range []*providercall.ContentFetchResponse{tracks, albums} {
		if resp != nil {
			items = append(items, resp.Items...)
		}
	}
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = item.Title
	}
	return titles, nil
}
