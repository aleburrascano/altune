package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"context"
	"sync/atomic"
)

type CountingAlbumTracksProvider struct {
	TracksByAlbum map[string][]domain.SearchResult
	Calls         atomic.Int64
}

func (p *CountingAlbumTracksProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	p.Calls.Add(1)
	return p.TracksByAlbum[albumID], nil
}
