package ports

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type AcquisitionScheduler interface {
	Schedule(ctx context.Context, userId ids.UserId, trackId domain.TrackId, sourceURL string) error
}

func NoopAcquisitionScheduler() AcquisitionScheduler { return noopAcquisitionScheduler{} }

type noopAcquisitionScheduler struct{}

func (noopAcquisitionScheduler) Schedule(context.Context, ids.UserId, domain.TrackId, string) error {
	return nil
}
