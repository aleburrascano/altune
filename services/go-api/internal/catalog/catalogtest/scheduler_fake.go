package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
)

type Scheduler struct {
	TrackIds   []domain.TrackId
	SourceURLs []string
}

var _ ports.AcquisitionScheduler = (*Scheduler)(nil)

func (s *Scheduler) Schedule(_ context.Context, _ shared.UserId, trackId domain.TrackId, sourceURL string) {
	s.TrackIds = append(s.TrackIds, trackId)
	s.SourceURLs = append(s.SourceURLs, sourceURL)
}
