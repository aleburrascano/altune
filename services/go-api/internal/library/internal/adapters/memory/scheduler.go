package memory

import (
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
)

type Scheduler struct {
	TrackIds   []domain.TrackId
	SourceURLs []string
	Err        error
}

var _ ports.AcquisitionScheduler = (*Scheduler)(nil)

func (s *Scheduler) Schedule(_ context.Context, _ ids.UserId, trackId domain.TrackId, sourceURL string) error {
	s.TrackIds = append(s.TrackIds, trackId)
	s.SourceURLs = append(s.SourceURLs, sourceURL)
	return s.Err
}
