package memory

import (
	"altune/go-api/internal/search/internal/ports"
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
