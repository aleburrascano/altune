package memory

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"sync/atomic"
)

type CountingRelationshipQuerier struct {
	MatchesByUser map[ids.UserId][]ports.RelatedTrackMatch
	Calls         atomic.Int64
}

func (q *CountingRelationshipQuerier) FindRelatedByAlbumArtist(_ context.Context, userId ids.UserId, _, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
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
