package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}

// NoopAcquisitionScheduler discards every schedule request. Callers that
// don't wire a real AcquisitionScheduler can default to this instead of
// guarding every Schedule call against a nil field.
func NoopAcquisitionScheduler() AcquisitionScheduler { return noopAcquisitionScheduler{} }

type noopAcquisitionScheduler struct{}

func (noopAcquisitionScheduler) Schedule(shared.UserId, domain.TrackId, string) {}
