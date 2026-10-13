package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"sync/atomic"
)

type CountingRelationshipQuerier struct {
	MatchesByUser map[shared.UserId][]ports.RelatedTrackMatch
	Calls         atomic.Int64
}

func (q *CountingRelationshipQuerier) FindRelatedByAlbum(_ context.Context, userId shared.UserId, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	q.Calls.Add(1)
	return q.MatchesByUser[userId], nil
}

type CountingAlbumTracksProvider struct {
	TracksByAlbum map[string][]domain.SearchResult
	Calls         atomic.Int64
}

func (p *CountingAlbumTracksProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	p.Calls.Add(1)
	return p.TracksByAlbum[albumID], nil
}
