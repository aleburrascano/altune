package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}

func NoopAcquisitionScheduler() AcquisitionScheduler { return noopAcquisitionScheduler{} }

type noopAcquisitionScheduler struct{}

func (noopAcquisitionScheduler) Schedule(shared.UserId, domain.TrackId, string) {}
