package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
)

type AlbumContentProvider struct {
	Results []domain.SearchResult
	Err     error

	GetAlbumTracksFn func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

var _ ports.AlbumContentProvider = (*AlbumContentProvider)(nil)

func (p *AlbumContentProvider) GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	if p.GetAlbumTracksFn != nil {
		return p.GetAlbumTracksFn(ctx, provider, externalID)
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return p.Results, nil
}
