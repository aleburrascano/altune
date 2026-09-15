package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

// AcquisitionScheduler queues a background acquisition for a track. A nil error
// means a job for the track is queued or already in flight; a non-nil error
// means the job was refused (queue full, shutting down) and nothing was queued.
type AcquisitionScheduler interface {
	Schedule(ctx context.Context, userId shared.UserId, trackId domain.TrackId, sourceURL string) error
}

func NoopAcquisitionScheduler() AcquisitionScheduler { return noopAcquisitionScheduler{} }

type noopAcquisitionScheduler struct{}

func (noopAcquisitionScheduler) Schedule(context.Context, shared.UserId, domain.TrackId, string) error {
	return nil
}
