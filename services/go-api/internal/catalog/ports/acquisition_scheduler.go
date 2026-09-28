package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

type AcquisitionScheduler interface {
	Schedule(ctx context.Context, userId shared.UserId, trackId domain.TrackId, sourceURL string) error
}

func NoopAcquisitionScheduler() AcquisitionScheduler { return noopAcquisitionScheduler{} }

type noopAcquisitionScheduler struct{}

func (noopAcquisitionScheduler) Schedule(context.Context, shared.UserId, domain.TrackId, string) error {
	return nil
}
