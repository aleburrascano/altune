package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
)

// Scheduler records every Schedule call. When Err is set, calls are still
// recorded but report Err, standing in for a scheduler that refused the job.
type Scheduler struct {
	TrackIds   []domain.TrackId
	SourceURLs []string
	Err        error
}

var _ ports.AcquisitionScheduler = (*Scheduler)(nil)

func (s *Scheduler) Schedule(_ context.Context, _ shared.UserId, trackId domain.TrackId, sourceURL string) error {
	s.TrackIds = append(s.TrackIds, trackId)
	s.SourceURLs = append(s.SourceURLs, sourceURL)
	return s.Err
}
